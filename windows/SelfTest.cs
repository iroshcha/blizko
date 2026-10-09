using System;
using System.IO;
using System.Linq;
using System.Text;
using System.Threading;
using System.Windows;
using System.Windows.Media;
using System.Windows.Media.Imaging;

namespace Blizko {
    internal static class SelfTest {
        private static Snapshot WaitDelivered(Engine engine, string text) {
            DateTime deadline = DateTime.UtcNow.AddSeconds(90);
            while (DateTime.UtcNow < deadline) {
                Snapshot state = engine.Request("snapshot").GetAwaiter().GetResult().snapshot;
                if (state.messages.Any(m => m.@out && m.delivered && m.text == text)) return state;
                Thread.Sleep(300);
            }
            throw new Exception("No authenticated receipt within 90 seconds");
        }
        internal static int Network(string directory) {
            Directory.CreateDirectory(directory); var log = new StringBuilder();
            try {
                using (var a = new Engine(Path.Combine(directory, "a"))) {
                    string aCode = a.Request("code").GetAwaiter().GetResult().code;
                    string aId = a.Request("snapshot").GetAwaiter().GetResult().snapshot.id;
                    string bId, bCode;
                    using (var b = new Engine(Path.Combine(directory, "b"))) {
                        bCode = b.Request("code").GetAwaiter().GetResult().code;
                        bId = b.Request("snapshot").GetAwaiter().GetResult().snapshot.id;
                        a.Request("add", "Друг B", bCode).GetAwaiter().GetResult();
                        b.Request("add", "Друг A", aCode).GetAwaiter().GetResult();
                        foreach (bool relay in new[] { true, false }) {
                            a.Request("relay", value: relay).GetAwaiter().GetResult();
                            b.Request("relay", value: relay).GetAwaiter().GetResult();
                            a.Request("start").GetAwaiter().GetResult(); b.Request("start").GetAwaiter().GetResult();
                            string first = "Привет с Windows 👋 " + relay, second = "Ответ с Windows 🌿 " + relay;
                            a.Request("send", bId, first).GetAwaiter().GetResult(); b.Request("send", aId, second).GetAwaiter().GetResult();
                            WaitDelivered(a, first); WaitDelivered(b, second);
                            log.AppendLine("PASS: two Windows processes, Unicode, authenticated delivery, relayOnly=" + relay);
                            a.Request("stop").GetAwaiter().GetResult(); b.Request("stop").GetAwaiter().GetResult();
                        }
                        b.Request("quit").GetAwaiter().GetResult();
                    }
                    a.Request("relay", value: true).GetAwaiter().GetResult(); a.Request("start").GetAwaiter().GetResult();
                    string queued = "Сохранено, пока собеседник выключен";
                    a.Request("send", bId, queued).GetAwaiter().GetResult();
                    Snapshot offline = a.Request("snapshot").GetAwaiter().GetResult().snapshot;
                    if (!offline.messages.Any(m => m.text == queued && !m.delivered)) throw new Exception("Offline queue missing");
                    using (var restarted = new Engine(Path.Combine(directory, "b"))) {
                        if (restarted.Request("code").GetAwaiter().GetResult().code != bCode) throw new Exception("Peer identity changed after process restart");
                        restarted.Request("relay", value: true).GetAwaiter().GetResult(); restarted.Request("start").GetAwaiter().GetResult();
                        WaitDelivered(a, queued);
                        string check = a.Request("check", bId).GetAwaiter().GetResult().code;
                        if (!check.Contains("подтвердил ваш контакт")) throw new Exception("Contact probe failed");
                        Snapshot received = restarted.Request("snapshot").GetAwaiter().GetResult().snapshot;
                        if (received.messages.Count(m => !m.@out && m.text == queued) != 1) throw new Exception("Queued message duplicated");
                        restarted.Request("quit").GetAwaiter().GetResult();
                    }
                    a.Request("quit").GetAwaiter().GetResult();
                    log.AppendLine("PASS: process restart preserves QR, queued message arrives once, contact probe succeeds");
                }
                File.WriteAllText(Path.Combine(directory, "network-test.txt"), log.ToString(), Encoding.UTF8); return 0;
            } catch (Exception e) {
                log.AppendLine("FAIL: " + e); File.WriteAllText(Path.Combine(directory, "network-test.txt"), log.ToString(), Encoding.UTF8); return 1;
            }
        }
        internal static int Preview(string path) {
            var application = new Application();
            var controller = new ChatWindow(true);
            controller.Apply(new Snapshot { status = "Приём выключен" });
            SavePreview(controller, path);
            return 0;
        }
        internal static void SavePreview(ChatWindow controller, string path) {
            var surface = (FrameworkElement)controller.Window.Content;
            surface.Measure(new Size(1080, 720)); surface.Arrange(new Rect(0, 0, 1080, 720)); surface.UpdateLayout();
            var bitmap = new RenderTargetBitmap(1080, 720, 96, 96, PixelFormats.Pbgra32); bitmap.Render(surface);
            var png = new PngBitmapEncoder(); png.Frames.Add(BitmapFrame.Create(bitmap));
            using (var file = File.Create(path)) png.Save(file);
        }
        internal static int Run(string directory) {
            Directory.CreateDirectory(directory);
            var log = new StringBuilder();
            try {
                using (var engine = new Engine(Path.Combine(directory, "engine"))) {
                    var snapshot = engine.Request("snapshot").GetAwaiter().GetResult().snapshot;
                    if (snapshot == null || snapshot.contacts.Count != 0) throw new Exception("Initial snapshot failed");
                    string code = engine.Request("code").GetAwaiter().GetResult().code;
                    using (var qr = Qr.Create(code)) qr.Save(Path.Combine(directory, "contact.png"), System.Drawing.Imaging.ImageFormat.Png);
                    if (Qr.Decode(Path.Combine(directory, "contact.png")) != code) throw new Exception("QR image round trip failed");
                    log.AppendLine("PASS: Windows UI -> Go engine -> QR PNG -> decoded contact");
                    bool rejected = false;
                    try { engine.Request("add", "Self", code).GetAwaiter().GetResult(); } catch (IOException) { rejected = true; }
                    if (!rejected) throw new Exception("Own contact accepted");
                    log.AppendLine("PASS: own-contact rejection reaches Windows interface");
                    engine.Request("quit").GetAwaiter().GetResult();
                }
                using (var restarted = new Engine(Path.Combine(directory, "engine"))) {
                    string persisted = restarted.Request("code").GetAwaiter().GetResult().code;
                    if (persisted != Qr.Decode(Path.Combine(directory, "contact.png"))) throw new Exception("Identity changed after restart");
                    restarted.Request("quit").GetAwaiter().GetResult();
                    log.AppendLine("PASS: silent storage-key loading and identity across process restart");
                }
                var application = new Application();
                var controller = new ChatWindow(true);
                controller.Apply(new Snapshot { status = "Приём выключен" });
                SavePreview(controller, Path.Combine(directory, "windows-preview.png"));
                log.AppendLine("PASS: native WPF window loads and renders");
                long now = (long)(DateTime.UtcNow - new DateTime(1970, 1, 1, 0, 0, 0, DateTimeKind.Utc)).TotalMilliseconds;
                var conversation = new Snapshot { status = "Подключено · iroh", enabled = true, online = true };
                conversation.contacts.Add(new Contact { id = "preview", name = "Анна" });
                conversation.messages.Add(new ChatMessage { id = "one", peer = "preview", text = "Привет! Как твой день? 🌿", time = now - 120000 });
                conversation.messages.Add(new ChatMessage { id = "two", peer = "preview", text = "Привет! Пишу тебе с компьютера. Всё рядом — и без регистрации.", @out = true, delivered = true, time = now - 60000 });
                conversation.messages.Add(new ChatMessage { id = "three", peer = "preview", text = "Встретимся вечером?", @out = true, time = now });
                controller.Apply(conversation);
                ((System.Windows.Controls.ListBox)controller.Window.FindName("Contacts")).SelectedIndex = 0;
                var compose = (System.Windows.Controls.TextBox)controller.Window.FindName("Compose");
                compose.Text = "До встречи!";
                controller.Apply(conversation);
                if (compose.Text != "До встречи!" || !compose.IsEnabled) throw new Exception("Conversation draft lost during refresh");
                SavePreview(controller, Path.Combine(directory, "windows-chat-preview.png"));
                log.AppendLine("PASS: contact selection, conversation, delivery states and draft preserved during refresh");
                File.WriteAllText(Path.Combine(directory, "self-test.txt"), log.ToString(), Encoding.UTF8);
                return 0;
            } catch (Exception e) {
                log.AppendLine("FAIL: " + e);
                File.WriteAllText(Path.Combine(directory, "self-test.txt"), log.ToString(), Encoding.UTF8);
                return 1;
            }
        }
    }
}
