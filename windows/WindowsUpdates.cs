using System;
using System.Diagnostics;
using System.IO;
using System.Net;
using System.Net.Http;
using System.Text;
using System.Threading;
using System.Threading.Tasks;

namespace Blizko {
    internal sealed class UpdatePreferences {
        public bool automatic { get; set; }
        public long lastCheck { get; set; }
        public string notified { get; set; }
        public UpdatePreferences() { automatic = true; }
    }
    internal sealed class UpdatePlan {
        public string action { get; set; }
        public string target { get; set; }
        public int parent { get; set; }
        public long parentStarted { get; set; }
        public bool receive { get; set; }
    }
    internal sealed class PreparedUpdate {
        internal string Directory;
        internal string Manifest;
        internal UpdateRelease Release;
    }
    internal static class WindowsUpdates {
        internal const string Feed = "https://github.com/iroshcha/blizko/releases/latest/download/windows-update.json";
        internal static string Root { get { return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "BlizkoUpdates"); } }
        internal static UpdatePreferences LoadPreferences() {
            try { string path = Path.Combine(Root, "preferences.json"); UpdatePackage.NoLinks(path); if (new FileInfo(path).Length <= 65536) return UpdatePackage.Json().Deserialize<UpdatePreferences>(File.ReadAllText(path)) ?? new UpdatePreferences(); } catch (Exception) { }
            return new UpdatePreferences();
        }
        internal static void SavePreferences(UpdatePreferences preferences) { EnsureRoot(); UpdatePackage.WriteAtomic(Path.Combine(Root, "preferences.json"), Encoding.UTF8.GetBytes(UpdatePackage.Json().Serialize(preferences))); }
        internal static string CachedManifest() {
            try { string path = Path.Combine(Root, "latest.json"); UpdatePackage.NoLinks(path); if (new FileInfo(path).Length <= 65536) { string raw = File.ReadAllText(path); UpdatePackage.Verify(raw); return raw; } } catch (Exception) { }
            return null;
        }
        internal static void EnsureRoot() { UpdatePackage.NoLinks(Root); Directory.CreateDirectory(Root); }
        internal static void PruneCache() {
            try { EnsureRoot(); foreach (string directory in Directory.GetDirectories(Root)) {
                Guid id; if (Guid.TryParseExact(Path.GetFileName(directory), "N", out id) && System.IO.Directory.GetLastWriteTimeUtc(directory) < DateTime.UtcNow.AddDays(-1)) Discard(new PreparedUpdate { Directory = directory });
            } } catch (Exception) { }
        }
        internal static async Task<HttpResponseMessage> Get(HttpClient client, string address, CancellationToken cancel) {
            for (int attempt = 0; attempt < 6; attempt++) {
                Uri uri = new Uri(address);
                if (uri.Scheme != "https" || !uri.IsDefaultPort || uri.UserInfo != "" || (uri.Host != "github.com" && uri.Host != "release-assets.githubusercontent.com" && uri.Host != "objects.githubusercontent.com")) throw new IOException("Недопустимый адрес загрузки обновления.");
                var request = new HttpRequestMessage(HttpMethod.Get, uri); request.Headers.UserAgent.ParseAdd("Blizko-Windows-Updater/1"); request.Headers.CacheControl = new System.Net.Http.Headers.CacheControlHeaderValue { NoCache = true };
                HttpResponseMessage response;
                using (request) response = await client.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, cancel).ConfigureAwait(false);
                int status = (int)response.StatusCode;
                if (status >= 300 && status <= 399 && response.Headers.Location != null) { address = new Uri(uri, response.Headers.Location).AbsoluteUri; response.Dispose(); continue; }
                if (!response.IsSuccessStatusCode) { response.Dispose(); throw new IOException("Не удалось получить обновление. Проверьте интернет и попробуйте ещё раз."); }
                return response;
            }
            throw new IOException("Слишком много перенаправлений при загрузке обновления.");
        }
        internal static HttpClient Client() {
            ServicePointManager.SecurityProtocol |= SecurityProtocolType.Tls12;
            return new HttpClient(new HttpClientHandler { AllowAutoRedirect = false, UseCookies = false }) { Timeout = Timeout.InfiniteTimeSpan };
        }
        internal static async Task<string> Check(CancellationToken cancel) {
            using (var deadline = CancellationTokenSource.CreateLinkedTokenSource(cancel)) using (var client = Client()) {
                deadline.CancelAfter(TimeSpan.FromSeconds(30));
                using (var response = await Get(client, Feed + "?check=" + DateTime.UtcNow.Ticks, deadline.Token).ConfigureAwait(false))
                using (var input = await response.Content.ReadAsStreamAsync().ConfigureAwait(false)) using (var output = new MemoryStream()) {
                    var bytes = new byte[8192]; int count;
                    while ((count = await input.ReadAsync(bytes, 0, bytes.Length, deadline.Token).ConfigureAwait(false)) != 0) { if (output.Length + count > 65536) throw new InvalidDataException("Описание обновления слишком большое."); output.Write(bytes, 0, count); }
                    string raw = new UTF8Encoding(false, true).GetString(output.ToArray()); UpdatePackage.Verify(raw);
                    EnsureRoot(); UpdatePackage.WriteAtomic(Path.Combine(Root, "latest.json"), Encoding.UTF8.GetBytes(raw)); return raw;
                }
            }
        }
        internal static async Task<PreparedUpdate> Download(string manifest, IProgress<int> progress, CancellationToken cancel) {
            var release = UpdatePackage.Verify(manifest);
            if (release.Version <= UpdatePackage.CurrentVersion) throw new InvalidDataException("Установлена эта или более новая версия.");
            EnsureRoot(); string directory = Path.Combine(Root, Guid.NewGuid().ToString("N")); Directory.CreateDirectory(directory);
            var prepared = new PreparedUpdate { Directory = directory, Manifest = manifest, Release = release };
            try {
                using (var deadline = CancellationTokenSource.CreateLinkedTokenSource(cancel)) using (var client = Client()) {
                    deadline.CancelAfter(TimeSpan.FromMinutes(10));
                    using (var response = await Get(client, release.url, deadline.Token).ConfigureAwait(false)) {
                        if (response.Content.Headers.ContentLength.HasValue && response.Content.Headers.ContentLength.Value != release.size) throw new InvalidDataException("Размер загрузки не совпадает с описанием обновления.");
                        using (var input = await response.Content.ReadAsStreamAsync().ConfigureAwait(false)) using (var output = new FileStream(Path.Combine(directory, "package.zip"), FileMode.CreateNew, FileAccess.Write, FileShare.None, 65536, true)) {
                            var bytes = new byte[65536]; long copied = 0; int count, reported = -1;
                            while ((count = await input.ReadAsync(bytes, 0, bytes.Length, deadline.Token).ConfigureAwait(false)) != 0) {
                                copied += count; if (copied > release.size) throw new InvalidDataException("Загрузка больше указанного размера.");
                                await output.WriteAsync(bytes, 0, count, deadline.Token).ConfigureAwait(false);
                                int percent = (int)(copied * 100 / release.size); if (percent != reported) { reported = percent; if (progress != null) progress.Report(percent); }
                            }
                            if (copied != release.size) throw new InvalidDataException("Обновление загружено не полностью."); output.Flush(true);
                        }
                    }
                }
                await Task.Run(() => { cancel.ThrowIfCancellationRequested(); UpdatePackage.Extract(Path.Combine(directory, "package.zip"), Path.Combine(directory, "verified"), release); }, cancel).ConfigureAwait(false);
                UpdatePackage.WriteAtomic(Path.Combine(directory, "manifest.json"), Encoding.UTF8.GetBytes(manifest)); return prepared;
            } catch { Discard(prepared); throw; }
        }
        internal static void Discard(PreparedUpdate update) {
            if (update == null) return;
            try { string path = Path.GetFullPath(update.Directory), root = Path.GetFullPath(Root) + Path.DirectorySeparatorChar;
                Guid id; if (!path.StartsWith(root, StringComparison.OrdinalIgnoreCase) || !Guid.TryParseExact(Path.GetFileName(path), "N", out id)) return;
                UpdatePackage.NoLinks(path); if (!System.IO.Directory.Exists(path)) return;
                foreach (string file in System.IO.Directory.GetFileSystemEntries(path, "*", SearchOption.AllDirectories)) UpdatePackage.NoLinks(file);
                System.IO.Directory.Delete(path, true);
            } catch (Exception) { }
        }
        internal static string Quote(string value) { return "\"" + value.Replace("\"", "") .TrimEnd('\\') + "\""; }
        internal static Process StartHelper(PreparedUpdate prepared, string target, bool receive, bool recover) {
            target = UpdatePackage.Target(target); EnsureRoot();
            string directory = prepared == null ? Path.Combine(Root, Guid.NewGuid().ToString("N")) : prepared.Directory;
            Directory.CreateDirectory(directory);
            foreach (string name in new[] { "Blizko.Updater.exe", "Blizko.Updater.exe.config" }) UpdatePackage.CopyDurable(Path.Combine(AppDomain.CurrentDomain.BaseDirectory, name), Path.Combine(directory, name));
            using (var parent = Process.GetCurrentProcess()) {
                var plan = new UpdatePlan { action = recover ? "recover" : "install", target = target, parent = parent.Id, parentStarted = parent.StartTime.ToUniversalTime().Ticks, receive = receive };
                UpdatePackage.WriteAtomic(Path.Combine(directory, "plan.json"), Encoding.UTF8.GetBytes(UpdatePackage.Json().Serialize(plan)));
            }
            var process = Process.Start(new ProcessStartInfo { FileName = Path.Combine(directory, "Blizko.Updater.exe"), Arguments = Quote(Path.Combine(directory, "plan.json")), WorkingDirectory = directory, UseShellExecute = false, CreateNoWindow = true });
            return process;
        }
        internal static async Task AwaitReady(Process process, string directory) {
            var until = DateTime.UtcNow.AddSeconds(15);
            while (DateTime.UtcNow < until) { if (process.HasExited) throw new IOException("Не удалось запустить установку. Приложение остаётся открытым."); if (File.Exists(Path.Combine(directory, "ready"))) return; await Task.Delay(100).ConfigureAwait(false); }
            if (!process.HasExited) process.Kill(); throw new IOException("Установщик не запустился вовремя. Приложение остаётся открытым.");
        }
        internal static void StartRecovery(string target, bool receive) {
            EnsureRoot(); var prepared = new PreparedUpdate { Directory = Path.Combine(Root, Guid.NewGuid().ToString("N")) };
            using (var process = StartHelper(prepared, target, receive, true)) AwaitReady(process, prepared.Directory).GetAwaiter().GetResult();
        }
    }
}
