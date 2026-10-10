using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Diagnostics;
using System.IO;
using System.Linq;
using System.Net.NetworkInformation;
using System.Reflection;
using System.Text;
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
        private readonly DispatcherTimer draftTimer = new DispatcherTimer { Interval=TimeSpan.FromMilliseconds(500) };
        private Snapshot snapshot = new Snapshot();
        private string peer = "", lastConversation = "", lastContacts = "";
        private long before, pageRevision = -1;
        private bool pageDirty = true;
        private bool openingUnread, checking, restoringDraft;
        private string historyQuery="", notificationPeer="";
        private readonly Dictionary<string,long> readOrders=new Dictionary<string,long>();
        private Dictionary<string,int> previousUnread;
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
            receive.Click += async delegate { await Action(async () => { Reply reply = await engine.Request(snapshot.enabled ? "stop" : "start"); ApplyStatus(reply.snapshot); }); };
            send.Click += async delegate { await Send(); };
            compose.PreviewKeyDown += async (sender, e) => { if (e.Key == Key.Enter && (Keyboard.Modifiers & ModifierKeys.Shift) == 0) { e.Handled = true; await Send(); } };
            compose.TextChanged += delegate {
                if(restoringDraft)return;
                string clipped=LimitText(compose.Text);if(clipped!=compose.Text){int caret=compose.CaretIndex;compose.Text=clipped;compose.CaretIndex=Math.Min(caret,clipped.Length);return;}
                if(peer!="")drafts[peer]=compose.Text;
                send.IsEnabled = !busy && !fatal && peer != "" && compose.Text.Trim().Length > 0;
                draftTimer.Stop();if(!preview&&peer!="")draftTimer.Start();
            };
            draftTimer.Tick += async delegate {draftTimer.Stop();await SaveDraft(peer);};
            check.Click += async delegate {
                if(engine==null)return;string target=peer;
                if(checking){await engine.Request("check-cancel",target);return;}
                checking=true;check.Content="Отменить проверку";
                try{Notice((await engine.Request("check",target)).code);}catch(Exception e){Notice(e.Message);}
                finally{checking=false;check.Content="Проверить связь";}
            };
            Find<Button>("Older").Click += async delegate { var rows = snapshot.messages.Where(m => m.peer == peer).ToList(); if (rows.Count > 0) { before = rows[0].order; pageDirty = true; await Poll(); } };
            Find<Button>("Recent").Click += async delegate { before = 0; historyQuery="";openingUnread=false;pageDirty = true; await Poll();scroll.ScrollToEnd();long latest;if(snapshot.latestOrder!=null&&snapshot.latestOrder.TryGetValue(peer,out latest))MarkVisibleRead(latest); };
            Find<Button>("FindMessages").Click += delegate {ShowMessageSearch();};
            Find<Button>("ClearHistory").Click += async delegate {
                if(snapshot.clear!=null&&snapshot.clear.running){await engine.Request("clear-cancel");return;}
                if (MessageBox.Show(Window, "Удалить тексты доставленных и отменённых сообщений на этом компьютере? Ожидающие отправки сообщения сохранятся.", "Очистить историю", MessageBoxButton.YesNo, MessageBoxImage.Question) == MessageBoxResult.Yes) {
                    string target = peer; await Action(async () => { await engine.Request("clear", target); before = 0; });
                }
            };
            Find<Button>("MyQR").Click += async delegate { await Action(async () => ShowQR((await engine.Request("code")).code)); };
            Find<Button>("AddContact").Click += delegate { ShowAddContact(); };
            Find<Button>("Settings").Click += delegate { ShowSettings(); };
            search.TextChanged += delegate { RenderContacts(true); };
            contacts.SelectionChanged += delegate {
                if (selecting) return;
                if (peer != "") {drafts[peer] = compose.Text;var save=SaveDraft(peer);}
                Contact contact = contacts.SelectedItem as Contact;
                peer = contact == null ? "" : contact.id;
                before = 0; historyQuery=""; openingUnread=true;pageDirty = true;
                restoringDraft=true;compose.Text = peer != "" && drafts.ContainsKey(peer) ? drafts[peer] : "";restoringDraft=false;
                lastConversation = ""; RenderConversation(); if (!preview) { var refresh = Poll(); }
            };
            scroll.ScrollChanged += delegate {if(!Window.IsActive||historyQuery!=""||peer=="")return;long visible=0;foreach(Border bubble in messages.Children.OfType<Border>()){if(!(bubble.Tag is long))continue;Point position=bubble.TransformToAncestor(scroll).Transform(new Point());if(position.Y<scroll.ViewportHeight&&position.Y+bubble.ActualHeight>0)visible=Math.Max(visible,(long)bubble.Tag);}MarkVisibleRead(visible);};
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
        private static string LimitText(string text){
            if(Encoding.UTF8.GetByteCount(text)<=4000)return text;int end=Math.Min(text.Length,4000);
            while(end>0&&Encoding.UTF8.GetByteCount(text.Substring(0,end))>4000)end--;
            if(end>0&&Char.IsHighSurrogate(text[end-1]))end--;return text.Substring(0,end);
        }
        private async Task SaveDraft(string target){
            if(preview||engine==null||target=="")return;string value; if(!drafts.TryGetValue(target,out value))return;
            try{await engine.Request("draft",target,value);}catch(Exception e){if(!exiting){issue.Text="Черновик не сохранён: "+e.Message;issueBox.Visibility=Visibility.Visible;}}
        }
        private void ShowMessageSearch(){
            if(peer=="")return;var dialog=Dialog("Поиск в переписке",420);var content=new StackPanel{Margin=new Thickness(24)};var field=new TextBox{Text=historyQuery,MaxLength=200};content.Children.Add(field);
            var find=new Button{Content="Найти",Margin=new Thickness(0,12,0,0)};find.Click+=async delegate{historyQuery=field.Text.Trim();before=0;openingUnread=false;pageDirty=true;dialog.Close();await Poll();};content.Children.Add(find);dialog.Content=content;dialog.ShowDialog();
        }
        private void Notice(string text) { MessageBox.Show(Window, text ?? "Не удалось выполнить действие.", "Близко", MessageBoxButton.OK, MessageBoxImage.Information); }
        private async Task Initialize() {
            try {
                engine = new Engine(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Blizko"));
                Reply initial = await engine.Request("snapshot"); Apply(initial.snapshot);
                ApplyStatus((await engine.Request("network-state",value:NetworkInterface.GetIsNetworkAvailable())).snapshot);
                tray = new Forms.NotifyIcon { Text = "Близко", Icon = System.Drawing.Icon.ExtractAssociatedIcon(Assembly.GetExecutingAssembly().Location), Visible = true };
                var menu = new Forms.ContextMenuStrip();
                menu.Items.Add("Открыть Близко", null, delegate { Restore(); });
                menu.Items.Add("Включить / выключить приём", null, async delegate { await Action(async () => ApplyStatus((await engine.Request(snapshot.enabled ? "stop" : "start")).snapshot)); });
                menu.Items.Add("Выйти", null, async delegate { await Exit(); });
                tray.ContextMenuStrip = menu;
                tray.DoubleClick += delegate { Restore(); };
                tray.BalloonTipClicked += delegate {Restore();Contact target=snapshot.contacts.FirstOrDefault(c=>c.id==notificationPeer);if(target!=null)contacts.SelectedItem=target;};
                timer.Start();
                await Action(async () => ApplyStatus((await engine.Request("start")).snapshot));
            } catch (Exception e) { fatal = true; status.Text = "Хранилище не открыто"; UpdateButtons(); Notice(e.Message); }
        }
        private void Restore() { Window.Show(); Window.WindowState = WindowState.Normal; Window.Activate(); pageDirty = true; var refresh = Poll(); }
        private async void Closing(object sender, CancelEventArgs e) {
            if (exiting) return;
            e.Cancel = true;
            if (fatal || tray == null) { await Exit(); return; }
            Window.Hide();
        }
        private async Task Exit() {
            if (exiting) return;draftTimer.Stop();await SaveDraft(peer);exiting = true; timer.Stop();
            NetworkChange.NetworkAddressChanged -= NetworkChanged;
            try { if (engine != null) await engine.Request("quit"); } catch (Exception) { }
            if (engine != null) engine.Dispose();
            if (tray != null) { tray.Visible = false; if (tray.Icon != null) tray.Icon.Dispose(); tray.Dispose(); }
            Window.Close(); Application.Current.Shutdown();
        }
        private void NetworkChanged(object sender, EventArgs e) {
            if (exiting || fatal || engine == null) return;
            Window.Dispatcher.BeginInvoke(new System.Action(async () => {
                try { ApplyStatus((await engine.Request("network-state",value:NetworkInterface.GetIsNetworkAvailable())).snapshot);ApplyStatus((await engine.Request("network")).snapshot); } catch (Exception) { }
            }));
        }
        private async Task Poll() {
            if (polling || exiting || fatal || engine == null) return;
            polling = true;
            try {
                Snapshot next = (await engine.Request("status")).snapshot; ApplyStatus(next);
                if (Window.IsVisible && (pageDirty || next.revision != pageRevision)) {
                    string target = peer; long cursor = before;
                    bool unread=openingUnread;string query=historyQuery;
                    Reply page = await engine.Request(unread?"unread":query==""?"page":"search", target,query,before: cursor);
                    if (peer == target && before == cursor && query==historyQuery) {if(unread){before=page.snapshot.before;openingUnread=false;}pageDirty = false; pageRevision = page.snapshot.revision; Apply(page.snapshot); }
                }
            }
            catch (Exception) { fatal = true; timer.Stop(); status.Text = "Приём остановлен · откройте приложение заново"; UpdateButtons(); }
            finally { polling = false; }
        }
        private async Task Action(Func<Task> action) {
            if (busy || exiting || fatal || engine == null) return;
            busy = true; UpdateButtons();
            try { await action(); } catch (Exception e) { Notice(e.Message); }
            finally { busy = false; pageDirty = true; UpdateButtons(); }
            await Poll();
        }
        private async Task Send() {
            if (peer == "" || compose.Text.Trim() == "" || DateTime.UtcNow - lastSend < TimeSpan.FromMilliseconds(200)) return;
            string target = peer, text = compose.Text;
            await Action(async () => {
                await SaveDraft(target);
                Reply reply = await engine.Request("send", target, text);
                string saved;if(drafts.TryGetValue(target,out saved)&&saved==text)drafts[target] = "";
                if (peer == target && compose.Text == text) compose.Clear();
                await engine.Request("draft-sent",target,text);
                lastSend = DateTime.UtcNow; ApplyStatus(reply.snapshot);
            });
        }
        private void UpdateButtons() {
            bool enabled = !busy && !fatal && (engine != null || preview);
            receive.IsEnabled = enabled;
            Find<Button>("MyQR").IsEnabled = enabled;
            Find<Button>("AddContact").IsEnabled = enabled;
            check.IsEnabled = !fatal && engine!=null && peer != "";
            compose.IsEnabled = !fatal && peer != "";
            send.IsEnabled = enabled && peer != "" && compose.Text.Trim().Length > 0;
        }
        internal void Apply(Snapshot next) {
            if (next == null) return;
            ApplyStatus(next); snapshot = next;
            RenderContacts(false); RenderConversation(); UpdateButtons();
        }
        private void ApplyStatus(Snapshot next) {
            if (next == null) return;
            snapshot.status = next.status; snapshot.enabled = next.enabled; snapshot.online = next.online; snapshot.relayOnly = next.relayOnly; snapshot.incoming = next.incoming; snapshot.relayURL = next.relayURL;
            snapshot.unread=next.unread;snapshot.firstUnread=next.firstUnread;snapshot.latestOrder=next.latestOrder;snapshot.sending=next.sending;snapshot.clear=next.clear;snapshot.storageIssue=next.storageIssue;
            status.Text = (snapshot.online ? "●  " : "○  ") + snapshot.status;
            receive.Content = snapshot.enabled ? "Выключить приём" : "Включить приём";
            if(tray!=null&&previousUnread!=null&&next.unread!=null)foreach(var entry in next.unread){int old;previousUnread.TryGetValue(entry.Key,out old);if(entry.Value>old&&(!Window.IsActive||entry.Key!=peer||before>0)){notificationPeer=entry.Key;tray.ShowBalloonTip(4000,"Близко · новое сообщение","Откройте Близко, чтобы прочитать сообщение.",Forms.ToolTipIcon.Info);break;}}
            previousUnread=next.unread==null?null:new Dictionary<string,int>(next.unread);
            incoming = snapshot.incoming;
            UpdateButtons();
        }
        private void RenderContacts(bool force) {
            foreach (Contact contact in snapshot.contacts) {
                string latest;
                contact.Preview = snapshot.previews != null && snapshot.previews.TryGetValue(contact.id, out latest) ? latest.Replace("\n", " ") : "Начать разговор";
                int unread;contact.Unread=snapshot.unread!=null&&snapshot.unread.TryGetValue(contact.id,out unread)?unread:0;
            }
            string key = search.Text + "\n" + String.Join("\n", snapshot.contacts.Select(c => c.id + "\0" + c.name + "\0" + c.Preview+":"+c.Unread));
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
            title.ToolTip=title.Text;
            if(contact!=null&&snapshot.peer==peer&&!drafts.ContainsKey(peer)){restoringDraft=true;drafts[peer]=snapshot.draft??"";compose.Text=drafts[peer];restoringDraft=false;}
            subtitle.Text = contact == null ? "Без регистрации. По вашему QR." : historyQuery==""?"Личная переписка · история на устройствах":"Поиск: "+historyQuery;
            welcome.Visibility = contact == null ? Visibility.Visible : Visibility.Collapsed;
            scroll.Visibility = contact == null ? Visibility.Collapsed : Visibility.Visible;
            Find<FrameworkElement>("HistoryControls").Visibility = contact == null ? Visibility.Collapsed : Visibility.Visible;
            Find<Button>("Older").IsEnabled = !busy && contact != null && snapshot.hasMore;
            Find<Button>("Recent").IsEnabled = !busy && contact != null && before > 0;
            long latestOrder;long lastDisplayed=snapshot.messages.Where(m=>m.peer==peer).Select(m=>m.order).DefaultIfEmpty(0).Max();
            bool newer=snapshot.latestOrder!=null&&snapshot.latestOrder.TryGetValue(peer,out latestOrder)&&latestOrder>lastDisplayed;
            int unreadCount;if(snapshot.unread!=null&&snapshot.unread.TryGetValue(peer,out unreadCount)&&unreadCount>0&&(before>0||scroll.ExtentHeight-scroll.VerticalOffset-scroll.ViewportHeight>40))newer=true;
            Find<Button>("Recent").Content=newer?"Новые сообщения ↓":"Последние";
            Find<Button>("Recent").IsEnabled=!busy&&contact!=null&&(before>0||historyQuery!=""||newer);
            Find<Button>("ClearHistory").Content=snapshot.clear!=null&&snapshot.clear.running?"Отменить очистку":"Очистить историю";
            Find<Button>("ClearHistory").IsEnabled = !busy && contact != null;
            string problem;
            bool hasIssue = snapshot.deliveryIssues != null && snapshot.deliveryIssues.TryGetValue(peer, out problem);
            issue.Text = hasIssue ? snapshot.deliveryIssues[peer] : "";
            if(snapshot.clear!=null&&snapshot.clear.peer==peer){if(snapshot.clear.running){issue.Text="Очистка: "+snapshot.clear.done+" / "+snapshot.clear.total;hasIssue=true;}else if(!String.IsNullOrEmpty(snapshot.clear.error)){issue.Text="Очистка прервана: "+snapshot.clear.error;hasIssue=true;}}
            if(!String.IsNullOrEmpty(snapshot.storageIssue)){issue.Text=snapshot.storageIssue;hasIssue=true;}
            issueBox.Visibility = hasIssue ? Visibility.Visible : Visibility.Collapsed;
            UpdateButtons();
            var conversation = snapshot.messages.Where(m => m.peer == peer).ToList();
            string key = peer + ":"+before+":"+historyQuery+"\n" + String.Join("\n", conversation.Select(m => m.id + ":" + m.delivered+":"+m.cancelled+":"+m.failure+":"+(snapshot.sending!=null&&snapshot.sending.ContainsKey(peer)?snapshot.sending[peer]:"")));
            if (key == lastConversation) return;bool changedPage=!lastConversation.StartsWith(peer+":"+before+":"+historyQuery+"\n",StringComparison.Ordinal); lastConversation = key;
            double offset = scroll.VerticalOffset;
            bool atEnd = scroll.ExtentHeight - scroll.VerticalOffset - scroll.ViewportHeight < 40;
            messages.Children.Clear();
            if (contact != null && conversation.Count == 0) messages.Children.Add(new TextBlock { Text = "Ваш разговор начинается здесь.", Foreground = Muted, Margin = new Thickness(0, 24, 0, 0) });
            string previousDate="";long unreadFirst;snapshot.firstUnread=snapshot.firstUnread??new Dictionary<string,long>();snapshot.firstUnread.TryGetValue(peer,out unreadFirst);
            foreach (ChatMessage message in conversation) {
                var date=new DateTime(1970,1,1,0,0,0,DateTimeKind.Utc).AddMilliseconds(message.time).ToLocalTime();string day=date.ToString("d MMMM yyyy");
                if(day!=previousDate){messages.Children.Add(new TextBlock{Text=day,HorizontalAlignment=HorizontalAlignment.Center,Foreground=Muted,Margin=new Thickness(0,10,0,16)});previousDate=day;}
                if(unreadFirst>0&&message.order==unreadFirst)messages.Children.Add(new TextBlock{Text="Непрочитанные",Foreground=Green,HorizontalAlignment=HorizontalAlignment.Center,Margin=new Thickness(0,4,0,12)});
                var content = new StackPanel();
                content.Children.Add(new TextBlock { Text = message.text, TextWrapping = TextWrapping.Wrap, FontSize = 15 });
                string time = new DateTime(1970, 1, 1, 0, 0, 0, DateTimeKind.Utc).AddMilliseconds(message.time).ToLocalTime().ToString("HH:mm");
                if (message.@out) time += message.delivered ? " · Доставлено" :message.cancelled?" · Отменено":!String.IsNullOrEmpty(message.failure)?" · Не отправлено":snapshot.sending!=null&&snapshot.sending.ContainsKey(peer)&&snapshot.sending[peer]==message.id?" · Отправляем…":!snapshot.enabled?" · Ожидает включения приёма":" · В очереди";
                if(!String.IsNullOrEmpty(message.failure))content.Children.Add(Label(message.failure));
                content.Children.Add(new TextBlock { Text = time, FontSize = 11, Foreground = Muted, Margin = new Thickness(0, 7, 0, 0) });
                var bubble=new Border { Child = content,Tag=message.order, Padding = new Thickness(16, 12, 16, 12), CornerRadius = new CornerRadius(14),
                    Background = message.@out ? new SolidColorBrush(Color.FromRgb(220, 238, 228)) : Brushes.White,
                    HorizontalAlignment = message.@out ? HorizontalAlignment.Right : HorizontalAlignment.Left,
                    MaxWidth = Math.Max(260, Window.ActualWidth > 0 ? (Window.ActualWidth - 380) * 0.85 : 480), Margin = new Thickness(0, 0, 0, 10) };
                var menu=new ContextMenu();var copy=new MenuItem{Header="Копировать текст"};copy.Click+=delegate{Clipboard.SetText(message.text);};menu.Items.Add(copy);
                if(message.@out&&!message.delivered&&!preview){var retry=new MenuItem{Header="Повторить отправку"};retry.Click+=async delegate{await Action(async()=>{await engine.Request("retry",message.peer,message.id);});};menu.Items.Add(retry);
                    if(!message.cancelled){var cancel=new MenuItem{Header="Отменить отправку"};cancel.Click+=async delegate{if(MessageBox.Show(Window,"Повторные попытки прекратятся. Если собеседник уже получил сообщение, отмена не удалит его у него.","Отменить отправку?",MessageBoxButton.YesNo,MessageBoxImage.Question)==MessageBoxResult.Yes)await Action(async()=>{await engine.Request("cancel",message.peer,message.id);});};menu.Items.Add(cancel);}}
                bubble.ContextMenu=menu;messages.Children.Add(bubble);
            }
            if (before == 0 && historyQuery=="" && (atEnd || conversation.Count < 2)) scroll.Dispatcher.BeginInvoke(new System.Action(()=>{scroll.ScrollToEnd();MarkVisibleRead(lastDisplayed);}), DispatcherPriority.Loaded);
            else scroll.Dispatcher.BeginInvoke(new System.Action(() => scroll.ScrollToVerticalOffset(changedPage?0:offset)), DispatcherPriority.Loaded);
        }
        private async void MarkVisibleRead(long order){
            if(preview||engine==null||!Window.IsActive||peer==""||historyQuery!="")return;long read;readOrders.TryGetValue(peer,out read);if(order<=read)return;string target=peer;readOrders[target]=order;
            try{await engine.Request("read",target,before:order);}catch(Exception){readOrders.Remove(target);}
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
                try { string value = Qr.Require(code.Text); Reply reply = await engine.Request("add", name.Text, value); ApplyStatus(reply.snapshot); pageDirty = true; await Poll(); dialog.Close(); }
                catch (Exception e) { hint.Text = e.Message; }
                finally { add.IsEnabled = true; }
            };
            dialog.Content = content; dialog.ShowDialog();
        }
        private void ShowSettings() {
            var dialog = Dialog("Настройки Близко", 470); var content = new StackPanel { Margin = new Thickness(24) };
            content.Children.Add(new TextBlock { Text = "Соединение", FontSize = 21, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 16) });
            var relay = new CheckBox { Content = "Только через ретранслятор", IsChecked = snapshot.relayOnly, Margin = new Thickness(0, 0, 0, 12) }; content.Children.Add(relay);
            content.Children.Add(Label("Адрес домашнего сервера (одинаковый на всех устройствах)"));
            var server = new TextBox { Text = snapshot.relayURL ?? "", Margin = new Thickness(0, 0, 0, 12) }; content.Children.Add(server);
            content.Children.Add(Label("В обычном режиме приложение выбирает прямое соединение или зашифрованный ретранслятор автоматически. Для доставки оба устройства должны быть в интернете."));
            var apply = new Button { Content = "Сохранить", Background = Green, Foreground = Brushes.White, IsEnabled = engine != null && !fatal };
            apply.Click += async delegate { apply.IsEnabled = false; try { await engine.Request("server",server.Text); ApplyStatus((await engine.Request("relay", value: relay.IsChecked == true)).snapshot); pageDirty = true; await Poll(); dialog.Close(); } catch (Exception e) { Notice(e.Message); apply.IsEnabled = true; } };
            content.Children.Add(apply);
            content.Children.Add(new TextBlock { Text = "Близко 0.2.4 · Windows", FontSize = 18, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 24, 0, 12) });
            content.Children.Add(Label("Регистрация не нужна. Ключи создаются автоматически. История хранится на этом компьютере. Закрытие окна оставляет приложение в трее; «Выйти» останавливает приём."));
            content.Children.Add(Label("Windows — отдельный контакт; синхронизации с вашей историей на телефоне пока нет. На iPhone для доставки нужно открыть приложение."));
            var release = new Button { Content = "Открыть страницу релизов", Background = Brushes.Transparent };
            release.Click += delegate { Process.Start(new ProcessStartInfo("https://github.com/iroshcha/blizko/releases") { UseShellExecute = true }); };
            content.Children.Add(release); dialog.Content = content; dialog.ShowDialog();
        }
    }
}
