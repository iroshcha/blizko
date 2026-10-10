using System;
using System.IO;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media;
using System.Windows.Threading;
using Forms = System.Windows.Forms;

namespace Blizko {
    internal sealed partial class ChatWindow {
        private readonly DispatcherTimer updateTimer = new DispatcherTimer { Interval = TimeSpan.FromMinutes(1) };
        private UpdatePreferences updatePreferences = new UpdatePreferences();
        private Window updatesWindow;
        private TextBlock updateStatus, updateNotes;
        private Button updateCheckButton, updateInstallButton, updateCancelButton;
        private ProgressBar updateProgress;
        private CancellationTokenSource updateDownload;
        private string updateManifest, updateMessage = "Проверим, есть ли новая версия.";
        private UpdateRelease availableUpdate;
        private bool updateChecking, updateBalloon, updateInstalling;
        private DateTime updateAttempt = DateTime.MinValue;
        private bool HasUpdate { get { return availableUpdate != null && availableUpdate.Version > UpdatePackage.CurrentVersion; } }

        private void InitializeUpdates() {
            Task.Run((Action)WindowsUpdates.PruneCache);
            updatePreferences = WindowsUpdates.LoadPreferences();
            string cached = WindowsUpdates.CachedManifest(); if (cached != null) SetUpdate(cached);
            updateTimer.Tick += async delegate { await AutomaticUpdateCheck(); }; updateTimer.Start();
            NotifyUpdate(); var check = AutomaticUpdateCheck();
        }
        private async Task AutomaticUpdateCheck() {
            if (!updatePreferences.automatic || exiting || updateChecking || updateDownload != null || DateTime.UtcNow - updateAttempt < TimeSpan.FromMinutes(15)) return;
            long last = updatePreferences.lastCheck;
            if (last > 0 && last <= DateTime.UtcNow.Ticks && DateTime.UtcNow.Ticks - last < TimeSpan.FromHours(6).Ticks) return;
            await CheckUpdates();
        }
        private void SetUpdate(string manifest) {
            availableUpdate = UpdatePackage.Verify(manifest); updateManifest = manifest;
            updateMessage = HasUpdate ? "Доступна версия " + availableUpdate.Version.ToString(3) + "." : availableUpdate.Version < UpdatePackage.CurrentVersion ? "Установленная версия новее версии в канале обновлений." : "У вас последняя опубликованная версия.";
            RenderUpdate();
        }
        private void NotifyUpdate() {
            if (!HasUpdate || !updatePreferences.automatic || tray == null || updatePreferences.notified == availableUpdate.version) return;
            updateBalloon = true;
            tray.ShowBalloonTip(6000, "Близко · доступно обновление", "Версия " + availableUpdate.Version.ToString(3) + ". Нажмите, чтобы обновить приложение.", Forms.ToolTipIcon.Info);
            updatePreferences.notified = availableUpdate.version;
            try { WindowsUpdates.SavePreferences(updatePreferences); } catch (Exception) { }
        }
        private void RenderUpdate() {
            Find<Button>("Updates").Content = HasUpdate ? "Обновить · " + availableUpdate.Version.ToString(3) : "Обновления";
            if (updatesWindow == null) return;
            updateStatus.Text = updateMessage; updateNotes.Text = HasUpdate ? availableUpdate.notes ?? "" : "";
            updateCheckButton.IsEnabled = !updateChecking && updateDownload == null && !updateInstalling;
            updateInstallButton.Visibility = HasUpdate ? Visibility.Visible : Visibility.Collapsed;
            updateInstallButton.IsEnabled = !updateChecking && updateDownload == null && !updateInstalling;
            updateCancelButton.Visibility = updateDownload != null && !updateInstalling ? Visibility.Visible : Visibility.Collapsed;
            updateProgress.Visibility = updateDownload != null ? Visibility.Visible : Visibility.Collapsed;
        }
        private async Task CheckUpdates() {
            if (updateChecking || updateDownload != null || updateInstalling || exiting) return;
            updateChecking = true; updateAttempt = DateTime.UtcNow; updateMessage = "Проверяем обновления…"; RenderUpdate();
            try {
                SetUpdate(await WindowsUpdates.Check(CancellationToken.None)); updatePreferences.lastCheck = DateTime.UtcNow.Ticks;
                WindowsUpdates.SavePreferences(updatePreferences); NotifyUpdate();
            } catch (Exception e) { updateMessage = e is OperationCanceledException ? "Проверка заняла слишком много времени. Попробуйте ещё раз." : "Не удалось проверить обновления. " + e.Message; }
            finally { updateChecking = false; RenderUpdate(); }
        }
        private void ShowUpdates(bool show = true) {
            if (updatesWindow != null) { updatesWindow.Activate(); return; }
            updatesWindow = Dialog("Обновления Близко", 480);
            var content = new StackPanel { Margin = new Thickness(24) };
            content.Children.Add(new TextBlock { Text = "Обновления", FontSize = 25, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 8) });
            content.Children.Add(Label("Установлена версия " + UpdatePackage.CurrentVersion.ToString(3)));
            updateStatus = Label(updateMessage); content.Children.Add(updateStatus);
            updateNotes = Label(""); content.Children.Add(new ScrollViewer { Content = updateNotes, MaxHeight = 150, VerticalScrollBarVisibility = ScrollBarVisibility.Auto });
            updateProgress = new ProgressBar { Minimum = 0, Maximum = 100, Height = 8, Margin = new Thickness(0, 4, 0, 14) }; content.Children.Add(updateProgress);
            updateInstallButton = new Button { Content = "Обновить и перезапустить", Background = Green, Foreground = Brushes.White, Margin = new Thickness(0, 0, 0, 10) };
            updateInstallButton.Click += async delegate { await InstallUpdate(); }; content.Children.Add(updateInstallButton);
            updateCancelButton = new Button { Content = "Отменить загрузку", Margin = new Thickness(0, 0, 0, 10) };
            updateCancelButton.Click += delegate { if (updateDownload != null) updateDownload.Cancel(); }; content.Children.Add(updateCancelButton);
            updateCheckButton = new Button { Content = "Проверить обновления", Margin = new Thickness(0, 0, 0, 18) };
            updateCheckButton.Click += async delegate { await CheckUpdates(); }; content.Children.Add(updateCheckButton);
            var automatic = new CheckBox { Content = "Автоматически проверять обновления", IsChecked = updatePreferences.automatic, Margin = new Thickness(0, 0, 0, 12) };
            automatic.Click += async delegate { updatePreferences.automatic = automatic.IsChecked == true; try { WindowsUpdates.SavePreferences(updatePreferences); if (updatePreferences.automatic) await AutomaticUpdateCheck(); } catch (Exception e) { updateMessage = e.Message; RenderUpdate(); } }; content.Children.Add(automatic);
            content.Children.Add(Label("Проверка выполняется при работающем приложении раз в 6 часов. Установка начинается по вашей кнопке. Переписка и контакты сохраняются."));
            updatesWindow.Closing += (sender, e) => { if (updateInstalling) { e.Cancel = true; return; } if (updateDownload != null) updateDownload.Cancel(); };
            updatesWindow.Closed += delegate { updatesWindow = null; };
            updatesWindow.Content = content; RenderUpdate(); if(show)updatesWindow.Show();
            if (!preview) { var check = CheckUpdates(); }
        }
        private async Task InstallUpdate() {
            if (!HasUpdate || updateDownload != null || updateInstalling || exiting) return;
            string manifest = updateManifest; PreparedUpdate prepared = null; bool handedOff = false;
            updateDownload = new CancellationTokenSource(); var cancellation = updateDownload;
            updateProgress.Value = 0; updateMessage = "Загружаем обновление…"; RenderUpdate();
            try {
                await Task.Run(() => UpdatePackage.CheckWritable(AppDomain.CurrentDomain.BaseDirectory));
                prepared = await WindowsUpdates.Download(manifest, new Progress<int>(value => { if (updatesWindow != null) { updateProgress.Value = value; updateMessage = value == 100 ? "Проверяем загруженные файлы…" : "Загрузка: " + value + "%"; RenderUpdate(); } }), cancellation.Token);
                cancellation.Token.ThrowIfCancellationRequested();
                // A failure to save the current draft keeps the app open.
                if (engine != null && !fatal && peer != "") await engine.Request("draft", peer, compose.Text);
                cancellation.Token.ThrowIfCancellationRequested(); if(exiting)throw new OperationCanceledException();
                updateInstalling = true; updateMessage = "Сохраняем переписку и перезапускаем приложение…"; RenderUpdate();
                using (var installer = WindowsUpdates.StartHelper(prepared, AppDomain.CurrentDomain.BaseDirectory, snapshot.enabled, false)) await WindowsUpdates.AwaitReady(installer, prepared.Directory);
                handedOff = true; Window.IsEnabled = false; updateDownload = null;
                if (updatesWindow != null) { updateInstalling = false; updatesWindow.Close(); }
                await Exit();
            } catch (Exception e) { updateMessage = e is OperationCanceledException ? "Загрузка отменена. Приложение продолжает работать." : "Обновление не установлено. " + e.Message; }
            finally { updateInstalling = false; updateDownload = null; cancellation.Dispose(); if (!handedOff) WindowsUpdates.Discard(prepared); if (!exiting) RenderUpdate(); }
        }
    }
}
