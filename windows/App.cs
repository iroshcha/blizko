using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Net.NetworkInformation;
using System.Reflection;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Markup;
using System.Windows.Media;
using System.Windows.Threading;
using Microsoft.Win32;
using Forms = System.Windows.Forms;

namespace Blizko {
    internal static class Program {
        [STAThread]
        private static int Main(string[] args) {
            try {
                if (args.Length >= 2 && args[0] == "--self-test") return SelfTest.Run(args[1]);
                if (args.Length >= 2 && args[0] == "--preview") return SelfTest.Preview(args[1]);
                if (args.Length >= 2 && args[0] == "--network-test") return SelfTest.Network(args[1]);
                bool created;
                using (var mutex = new Mutex(true, "Local\\Blizko.Windows.UI", out created)) {
                    if (!created) { MessageBox.Show("Близко уже запущено. Откройте его значком в системном трее.", "Близко"); return 0; }
                    var application = new Application { ShutdownMode = ShutdownMode.OnExplicitShutdown };
                    var controller = new ChatWindow(false);
                    application.Run(controller.Window);
                }
                return 0;
            } catch (Exception) {
                MessageBox.Show("Не удалось открыть «Близко». Проверьте, что архив полностью распакован и файлы приложения находятся рядом. История сохранена.", "Близко");
                return 1;
            }
        }
    }

    internal sealed class ChatWindow {
        internal Window Window { get; private set; }
        private readonly bool preview;
        private Engine engine;
        private Forms.NotifyIcon tray;
        private readonly DispatcherTimer timer = new DispatcherTimer();
        private Snapshot snapshot = new Snapshot();
        private string peer = "", lastConversation = "", lastContacts = "";
        private readonly Dictionary<string, string> drafts = new Dictionary<string, string>();
        private bool polling, busy, exiting, fatal, selecting;
        private int incoming = -1;
        private DateTime lastSend = DateTime.MinValue;
        private TextBox compose, search;
        private ListBox contacts;
        private Button receive, send, check;
        private TextBlock status, title, subtitle, issue;
        private StackPanel messages;
        private ScrollViewer scroll;
        private Border issueBox;
        private FrameworkElement welcome;
        private static readonly Brush Green = new SolidColorBrush(Color.FromRgb(35, 108, 88));
        private static readonly Brush Muted = new SolidColorBrush(Color.FromRgb(101, 118, 111));

        internal ChatWindow(bool preview) {
            this.preview = preview;
            using (Stream source = Assembly.GetExecutingAssembly().GetManifestResourceStream("Blizko.MainWindow.xaml")) {
                Window = (Window)XamlReader.Load(source);
            }
            compose = Find<TextBox>("Compose"); search = Find<TextBox>("Search"); contacts = Find<ListBox>("Contacts");
            receive = Find<Button>("Receive"); send = Find<Button>("Send"); check = Find<Button>("CheckContact");
            status = Find<TextBlock>("Status"); title = Find<TextBlock>("ChatTitle"); subtitle = Find<TextBlock>("ChatSubtitle");
            issue = Find<TextBlock>("Issue"); issueBox = Find<Border>("IssueBox");
            messages = Find<StackPanel>("Messages"); scroll = Find<ScrollViewer>("MessageScroll"); welcome = Find<FrameworkElement>("Welcome");
            receive.Click += async delegate { await Action(async () => { Reply reply = await engine.Request(snapshot.enabled ? "stop" : "start"); Apply(reply.snapshot); }); };
            send.Click += async delegate { await Send(); };
            compose.PreviewKeyDown += async (sender, e) => { if (e.Key == Key.Enter && (Keyboard.Modifiers & ModifierKeys.Shift) == 0) { e.Handled = true; await Send(); } };
            compose.TextChanged += delegate { send.IsEnabled = !busy && !fatal && peer != "" && compose.Text.Trim().Length > 0; };
            check.Click += async delegate { string target = peer; await Action(async () => Notice((await engine.Request("check", target)).code)); };
            Find<Button>("MyQR").Click += async delegate { await Action(async () => ShowQR((await engine.Request("code")).code)); };
            Find<Button>("AddContact").Click += delegate { ShowAddContact(); };
            Find<Button>("Settings").Click += delegate { ShowSettings(); };
            search.TextChanged += delegate { RenderContacts(true); };
            contacts.SelectionChanged += delegate {
                if (selecting) return;
                if (peer != "") drafts[peer] = compose.Text;
                Contact contact = contacts.SelectedItem as Contact;
                peer = contact == null ? "" : contact.id;
                compose.Text = peer != "" && drafts.ContainsKey(peer) ? drafts[peer] : "";
                lastConversation = ""; RenderConversation();
            };
            if (!preview) {
                Window.Loaded += async delegate { await Initialize(); };
                Window.Closing += Closing;
                Window.StateChanged += delegate { if (Window.WindowState == WindowState.Minimized) Window.Hide(); };
                timer.Interval = TimeSpan.FromSeconds(1);
                timer.Tick += async delegate { await Poll(); };
                NetworkChange.NetworkAddressChanged += NetworkChanged;
            }
        }
        private T Find<T>(string name) where T : class { return Window.FindName(name) as T; }
        private void Notice(string text) { MessageBox.Show(Window, text ?? "Не удалось выполнить действие.", "Близко", MessageBoxButton.OK, MessageBoxImage.Information); }
        private async Task Initialize() {
            try {
                engine = new Engine(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Blizko"));
                Reply initial = await engine.Request("snapshot"); Apply(initial.snapshot);
                tray = new Forms.NotifyIcon { Text = "Близко", Icon = System.Drawing.Icon.ExtractAssociatedIcon(Assembly.GetExecutingAssembly().Location), Visible = true };
                var menu = new Forms.ContextMenuStrip();
                menu.Items.Add("Открыть Близко", null, delegate { Restore(); });
                menu.Items.Add("Включить / выключить приём", null, async delegate { await Action(async () => Apply((await engine.Request(snapshot.enabled ? "stop" : "start")).snapshot)); });
                menu.Items.Add("Выйти", null, async delegate { await Exit(); });
                tray.ContextMenuStrip = menu;
                tray.DoubleClick += delegate { Restore(); };
                timer.Start();
                await Action(async () => Apply((await engine.Request("start")).snapshot));
            } catch (Exception e) { fatal = true; status.Text = "Хранилище не открыто"; UpdateButtons(); Notice(e.Message); }
        }
        private void Restore() { Window.Show(); Window.WindowState = WindowState.Normal; Window.Activate(); }
        private async void Closing(object sender, CancelEventArgs e) {
            if (exiting) return;
            e.Cancel = true;
            if (fatal || tray == null) { await Exit(); return; }
            Window.Hide();
        }
        private async Task Exit() {
            if (exiting) return; exiting = true; timer.Stop();
            NetworkChange.NetworkAddressChanged -= NetworkChanged;
            try { if (engine != null) await engine.Request("quit"); } catch (Exception) { }
            if (engine != null) engine.Dispose();
            if (tray != null) { tray.Visible = false; if (tray.Icon != null) tray.Icon.Dispose(); tray.Dispose(); }
            Window.Close(); Application.Current.Shutdown();
        }
        private void NetworkChanged(object sender, EventArgs e) {
            if (exiting || fatal || engine == null) return;
            Window.Dispatcher.BeginInvoke(new System.Action(async () => {
                try { Apply((await engine.Request("network")).snapshot); } catch (Exception) { }
            }));
        }
        private async Task Poll() {
            if (polling || exiting || fatal || engine == null) return;
            polling = true;
            try { Apply((await engine.Request("snapshot")).snapshot); }
            catch (Exception) { fatal = true; timer.Stop(); status.Text = "Приём остановлен · откройте приложение заново"; UpdateButtons(); }
            finally { polling = false; }
        }
        private async Task Action(Func<Task> action) {
            if (busy || exiting || fatal || engine == null) return;
            busy = true; UpdateButtons();
            try { await action(); } catch (Exception e) { Notice(e.Message); }
            finally { busy = false; UpdateButtons(); }
        }
        private async Task Send() {
            if (peer == "" || compose.Text.Trim() == "" || DateTime.UtcNow - lastSend < TimeSpan.FromMilliseconds(200)) return;
            string target = peer, text = compose.Text;
            await Action(async () => {
                Reply reply = await engine.Request("send", target, text);
                drafts[target] = "";
                if (peer == target && compose.Text == text) compose.Clear();
                lastSend = DateTime.UtcNow; Apply(reply.snapshot);
            });
        }
        private void UpdateButtons() {
            bool enabled = !busy && !fatal && (engine != null || preview);
            receive.IsEnabled = enabled;
            Find<Button>("MyQR").IsEnabled = enabled;
            Find<Button>("AddContact").IsEnabled = enabled;
            check.IsEnabled = enabled && peer != "";
            compose.IsEnabled = !fatal && peer != "";
            send.IsEnabled = enabled && peer != "" && compose.Text.Trim().Length > 0;
        }
        internal void Apply(Snapshot next) {
            if (next == null) return;
            snapshot = next;
            status.Text = (snapshot.online ? "●  " : "○  ") + snapshot.status;
            receive.Content = snapshot.enabled ? "Выключить приём" : "Включить приём";
            if (tray != null && incoming >= 0 && snapshot.incoming > incoming && !Window.IsActive) {
                tray.ShowBalloonTip(4000, "Близко · новое сообщение", "Откройте Близко, чтобы прочитать сообщение.", Forms.ToolTipIcon.Info);
            }
            incoming = snapshot.incoming;
            RenderContacts(false); RenderConversation(); UpdateButtons();
        }
        private void RenderContacts(bool force) {
            foreach (Contact contact in snapshot.contacts) {
                ChatMessage latest = snapshot.messages.LastOrDefault(m => m.peer == contact.id);
                contact.Preview = latest == null ? "Начать разговор" : latest.text.Replace("\n", " ");
            }
            string key = search.Text + "\n" + String.Join("\n", snapshot.contacts.Select(c => c.id + "\0" + c.name + "\0" + c.Preview));
            if (!force && key == lastContacts) return; lastContacts = key;
            selecting = true;
            var visible = snapshot.contacts.Where(c => c.name.IndexOf(search.Text.Trim(), StringComparison.CurrentCultureIgnoreCase) >= 0).ToList();
            contacts.ItemsSource = visible; contacts.SelectedItem = visible.FirstOrDefault(c => c.id == peer);
            Find<TextBlock>("NoContacts").Text = search.Text.Trim() == "" ? "Здесь появятся ваши разговоры." : "Контакты не найдены.";
            Find<TextBlock>("NoContacts").Visibility = visible.Count == 0 ? Visibility.Visible : Visibility.Collapsed;
            selecting = false;
        }
        private void RenderConversation() {
            Contact contact = snapshot.contacts.FirstOrDefault(c => c.id == peer);
            title.Text = contact == null ? "Ваш первый разговор" : contact.name;
            subtitle.Text = contact == null ? "Без регистрации. По вашему QR." : "Личная переписка · история на устройствах";
            welcome.Visibility = contact == null ? Visibility.Visible : Visibility.Collapsed;
            scroll.Visibility = contact == null ? Visibility.Collapsed : Visibility.Visible;
            string problem;
            bool hasIssue = snapshot.deliveryIssues != null && snapshot.deliveryIssues.TryGetValue(peer, out problem);
            issue.Text = hasIssue ? snapshot.deliveryIssues[peer] : "";
            issueBox.Visibility = hasIssue ? Visibility.Visible : Visibility.Collapsed;
            UpdateButtons();
            var conversation = snapshot.messages.Where(m => m.peer == peer).ToList();
            string key = peer + "\n" + String.Join("\n", conversation.Select(m => m.id + ":" + m.delivered));
            if (key == lastConversation) return; lastConversation = key;
            bool atEnd = scroll.ExtentHeight - scroll.VerticalOffset - scroll.ViewportHeight < 40;
            messages.Children.Clear();
            if (contact != null && conversation.Count == 0) messages.Children.Add(new TextBlock { Text = "Ваш разговор начинается здесь.", Foreground = Muted, Margin = new Thickness(0, 24, 0, 0) });
            foreach (ChatMessage message in conversation) {
                var content = new StackPanel();
                content.Children.Add(new TextBlock { Text = message.text, TextWrapping = TextWrapping.Wrap, FontSize = 15 });
                string time = new DateTime(1970, 1, 1, 0, 0, 0, DateTimeKind.Utc).AddMilliseconds(message.time).ToLocalTime().ToString("HH:mm");
                if (message.@out) time += message.delivered ? " · Доставлено" : " · В очереди";
                content.Children.Add(new TextBlock { Text = time, FontSize = 11, Foreground = Muted, Margin = new Thickness(0, 7, 0, 0) });
                messages.Children.Add(new Border { Child = content, Padding = new Thickness(16, 12, 16, 12), CornerRadius = new CornerRadius(14),
                    Background = message.@out ? new SolidColorBrush(Color.FromRgb(220, 238, 228)) : Brushes.White,
                    HorizontalAlignment = message.@out ? HorizontalAlignment.Right : HorizontalAlignment.Left,
                    MaxWidth = Math.Max(260, Window.ActualWidth > 0 ? (Window.ActualWidth - 380) * 0.85 : 480), Margin = new Thickness(0, 0, 0, 10) });
            }
            if (atEnd || conversation.Count < 2) scroll.Dispatcher.BeginInvoke(new System.Action(scroll.ScrollToEnd), DispatcherPriority.Loaded);
        }
        private Window Dialog(string title, double width) {
            return new Window { Title = title, Owner = Window, Width = width, SizeToContent = SizeToContent.Height, ResizeMode = ResizeMode.NoResize,
                WindowStartupLocation = WindowStartupLocation.CenterOwner, Background = new SolidColorBrush(Color.FromRgb(245, 246, 242)),
                FontFamily = new FontFamily("Segoe UI"), FontSize = 14, Foreground = Window.Foreground, Resources = Window.Resources };
        }
        private static TextBlock Label(string text) { return new TextBlock { Text = text, TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 0, 0, 12), Foreground = Muted }; }
        private void ShowQR(string code) {
            var dialog = Dialog("Мой QR", 440); var content = new StackPanel { Margin = new Thickness(24) };
            using (var bitmap = Qr.Create(code)) {
                var image = Qr.Source(bitmap);
                content.Children.Add(new Image { Source = image, Width = 360, Height = 360, Margin = new Thickness(0, 0, 0, 18) });
                content.Children.Add(Label("Добавьте этот QR на телефоне друга и импортируйте его QR здесь. Вход в аккаунт не нужен."));
                var save = new Button { Content = "Сохранить QR-картинку", Margin = new Thickness(0, 0, 0, 10) };
                save.Click += delegate {
                    var chooser = new SaveFileDialog { FileName = "Близко-контакт.png", Filter = "PNG (*.png)|*.png" };
                    if (chooser.ShowDialog(dialog) == true) { try { bitmap.Save(chooser.FileName, System.Drawing.Imaging.ImageFormat.Png); } catch (Exception e) { Notice(e.Message); } }
                };
                content.Children.Add(save);
                var copy = new Button { Content = "Скопировать код", Background = Brushes.Transparent };
                copy.Click += delegate { try { Clipboard.SetText(code); } catch (Exception) { Notice("Не удалось открыть буфер обмена. Сохраните QR-картинку."); } };
                content.Children.Add(copy); dialog.Content = content; dialog.ShowDialog();
            }
        }
        private void ShowAddContact() {
            var dialog = Dialog("Новый контакт", 460); var content = new StackPanel { Margin = new Thickness(24) };
            content.Children.Add(Label("Выберите QR-картинку друга или вставьте скопированный код. Контакт сохраняется после подтверждения."));
            var load = new Button { Content = "Выбрать QR из картинки", Margin = new Thickness(0, 0, 0, 16) }; content.Children.Add(load);
            content.Children.Add(Label("Имя собеседника")); var name = new TextBox { MaxLength = 60, Margin = new Thickness(0, 0, 0, 16) }; content.Children.Add(name);
            content.Children.Add(Label("Код контакта")); var code = new TextBox { Height = 80, TextWrapping = TextWrapping.Wrap, MaxLength = 4096, VerticalScrollBarVisibility = ScrollBarVisibility.Auto, Margin = new Thickness(0, 0, 0, 16) }; content.Children.Add(code);
            var hint = Label(""); content.Children.Add(hint);
            var add = new Button { Content = "Добавить контакт", Background = Green, Foreground = Brushes.White }; content.Children.Add(add);
            load.Click += async delegate {
                var picker = new OpenFileDialog { Filter = "Изображения|*.png;*.jpg;*.jpeg;*.bmp;*.gif|Все файлы|*.*" };
                if (picker.ShowDialog(dialog) != true) return;
                load.IsEnabled = false;
                try { code.Text = await Task.Run(() => Qr.Decode(picker.FileName)); hint.Text = "QR считан. Подтвердите имя."; }
                catch (Exception e) { hint.Text = e.Message; }
                finally { load.IsEnabled = true; }
            };
            add.Click += async delegate {
                if (String.IsNullOrWhiteSpace(name.Text)) { hint.Text = "Введите имя собеседника."; return; }
                add.IsEnabled = false;
                try { string value = Qr.Require(code.Text); Reply reply = await engine.Request("add", name.Text, value); Apply(reply.snapshot); dialog.Close(); }
                catch (Exception e) { hint.Text = e.Message; }
                finally { add.IsEnabled = true; }
            };
            dialog.Content = content; dialog.ShowDialog();
        }
        private void ShowSettings() {
            var dialog = Dialog("Настройки Близко", 470); var content = new StackPanel { Margin = new Thickness(24) };
            content.Children.Add(new TextBlock { Text = "Соединение", FontSize = 21, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 16) });
            var relay = new CheckBox { Content = "Только через ретранслятор", IsChecked = snapshot.relayOnly, Margin = new Thickness(0, 0, 0, 12) }; content.Children.Add(relay);
            content.Children.Add(Label("В обычном режиме приложение выбирает прямое соединение или зашифрованный ретранслятор автоматически. Для доставки оба устройства должны быть в интернете."));
            var apply = new Button { Content = "Сохранить", Background = Green, Foreground = Brushes.White, IsEnabled = engine != null && !fatal };
            apply.Click += async delegate { apply.IsEnabled = false; try { Apply((await engine.Request("relay", value: relay.IsChecked == true)).snapshot); dialog.Close(); } catch (Exception e) { Notice(e.Message); apply.IsEnabled = true; } };
            content.Children.Add(apply);
            content.Children.Add(new TextBlock { Text = "Близко 0.2.1 · Windows", FontSize = 18, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 24, 0, 12) });
            content.Children.Add(Label("Регистрация не нужна. Ключи создаются автоматически. История хранится на этом компьютере. Закрытие окна оставляет приложение в трее; «Выйти» останавливает приём."));
            content.Children.Add(Label("Windows — отдельный контакт; синхронизации с вашей историей на телефоне пока нет. На iPhone для доставки нужно открыть приложение."));
            var release = new Button { Content = "Открыть страницу релизов", Background = Brushes.Transparent };
            release.Click += delegate { Process.Start(new ProcessStartInfo("https://github.com/iroshcha/blizko/releases") { UseShellExecute = true }); };
            content.Children.Add(release); dialog.Content = content; dialog.ShowDialog();
        }
    }
}
