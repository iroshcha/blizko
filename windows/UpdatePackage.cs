using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.IO.Compression;
using System.Linq;
using System.Security.Cryptography;
using System.Text;
using System.Web.Script.Serialization;

namespace Blizko {
    internal sealed class UpdateRelease {
        public string version { get; set; }
        public string url { get; set; }
        public string sha256 { get; set; }
        public long size { get; set; }
        public string notes { get; set; }
        internal Version Version { get { return System.Version.Parse(version); } }
    }
    internal sealed class SignedUpdate { public string payload { get; set; } public string signature { get; set; } }
    internal sealed class UpdateFile { public string name { get; set; } public string oldHash { get; set; } public string newHash { get; set; } }
    internal sealed class UpdateJournal { public int format { get; set; } public string workspace { get; set; } public List<UpdateFile> files { get; set; } }

    // Shared by the UI and the separate updater. Never accepts paths from a ZIP.
    internal static class UpdatePackage {
        internal const long MaxDownload = 150L * 1024 * 1024;
        internal const string JournalName = ".blizko-update-pending.json";
        internal static readonly string[] Files = { "Blizko.Engine.exe", "blizko_iroh.dll", "zxing.dll", "Blizko.Updater.exe", "Blizko.Updater.exe.config", "Blizko.exe.config", "Blizko.ico", "README.txt", "THIRD_PARTY_NOTICES.txt", "IROH_NOTICES.txt", "ZXING_NET_NOTICE.txt", "Blizko.exe" };
        internal static JavaScriptSerializer Json() { return new JavaScriptSerializer { MaxJsonLength = 65536 }; }
        internal static Version CurrentVersion { get { return typeof(UpdatePackage).Assembly.GetName().Version; } }
        internal static UpdateRelease Verify(string envelope) { return Verify(envelope, UpdateKey.PublicXml); }
        internal static UpdateRelease Verify(string envelope, string publicKey) {
            if (String.IsNullOrWhiteSpace(envelope) || envelope.Length > 65536) throw new InvalidDataException("Неверное описание обновления.");
            try {
                var signed = Json().Deserialize<SignedUpdate>(envelope);
                byte[] payload = Convert.FromBase64String(signed.payload), signature = Convert.FromBase64String(signed.signature);
                if (payload.Length > 32768) throw new InvalidDataException();
                using (var rsa = new RSACryptoServiceProvider()) {
                    rsa.PersistKeyInCsp = false; rsa.FromXmlString(publicKey);
                    if (!rsa.VerifyData(payload, CryptoConfig.MapNameToOID("SHA256"), signature)) throw new CryptographicException();
                }
                var release = Json().Deserialize<UpdateRelease>(new UTF8Encoding(false, true).GetString(payload));
                Version version; Uri url;
                if (release == null || !Version.TryParse(release.version, out version) || version.Build < 0 || version.Revision < 0 || version <= new Version(0, 0, 0, 0)
                    || release.size < 1 || release.size > MaxDownload || !HashValid(release.sha256)
                    || !Uri.TryCreate(release.url, UriKind.Absolute, out url) || url.Scheme != "https" || url.Host != "github.com" || !url.IsDefaultPort || url.UserInfo != "" || url.Query != "" || url.Fragment != ""
                    || !url.AbsolutePath.StartsWith("/iroshcha/blizko/releases/download/", StringComparison.Ordinal) || !url.AbsolutePath.EndsWith("/Blizko-Windows-x64.zip", StringComparison.Ordinal)
                    || (release.notes != null && release.notes.Length > 4000)) throw new InvalidDataException();
                return release;
            } catch (Exception e) {
                if (e is OutOfMemoryException) throw;
                throw new InvalidDataException("Не удалось проверить подпись или описание обновления. Установка отменена.", e);
            }
        }
        internal static bool HashValid(string value) { return value != null && value.Length == 64 && value.All(c => (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')); }
        internal static string Hash(string path) { using (var input = File.OpenRead(path)) using (var sha = SHA256.Create()) return BitConverter.ToString(sha.ComputeHash(input)).Replace("-", "").ToLowerInvariant(); }
        internal static void VerifyArchive(string path, UpdateRelease release) {
            NoLinks(path);
            if (new FileInfo(path).Length != release.size || Hash(path) != release.sha256) throw new InvalidDataException("Загруженный файл повреждён. Скачайте обновление заново.");
        }
        internal static void NoLinks(string path) {
            var current = Path.GetFullPath(path);
            while (!String.IsNullOrEmpty(current)) {
                if ((File.Exists(current) || Directory.Exists(current)) && (File.GetAttributes(current) & FileAttributes.ReparsePoint) != 0) throw new IOException("Обновление через ссылку на папку или файл не поддерживается.");
                current = Path.GetDirectoryName(current);
            }
        }
        internal static string Target(string path) {
            string full = Path.GetFullPath(path).TrimEnd(Path.DirectorySeparatorChar);
            string data = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Blizko");
            if (full.StartsWith("\\\\", StringComparison.Ordinal) || full.Length <= 3 || full.Equals(data, StringComparison.OrdinalIgnoreCase) || full.StartsWith(data + "\\", StringComparison.OrdinalIgnoreCase)) throw new IOException("Неверная папка приложения.");
            NoLinks(full);
            if (!File.Exists(Path.Combine(full, "Blizko.exe"))) throw new IOException("Приложение не найдено в папке обновления.");
            return full;
        }
        internal static void CheckWritable(string target) {
            target = Target(target); string probe = Path.Combine(target, ".blizko-write-" + Guid.NewGuid().ToString("N"));
            try { using (var stream = new FileStream(probe, FileMode.CreateNew, FileAccess.Write, FileShare.None)) stream.Flush(true); }
            catch (Exception e) { throw new IOException("Нет доступа к папке приложения. Распакуйте Близко в свою папку, например в «Документы», и запустите оттуда.", e); }
            finally { if (File.Exists(probe)) File.Delete(probe); }
        }
        internal static void WriteAtomic(string path, byte[] bytes) {
            NoLinks(path); string temp = path + "." + Guid.NewGuid().ToString("N") + ".tmp";
            try {
                using (var output = new FileStream(temp, FileMode.CreateNew, FileAccess.Write, FileShare.None)) { output.Write(bytes, 0, bytes.Length); output.Flush(true); }
                if (File.Exists(path)) File.Replace(temp, path, null); else File.Move(temp, path);
            } finally { if (File.Exists(temp)) File.Delete(temp); }
        }
        internal static void CopyDurable(string source, string target) {
            NoLinks(source); NoLinks(target);
            using (var input = File.OpenRead(source)) using (var output = new FileStream(target, FileMode.CreateNew, FileAccess.Write, FileShare.None)) { input.CopyTo(output); output.Flush(true); }
        }
        internal static void Extract(string zip, string stage, UpdateRelease release) {
            VerifyArchive(zip, release); NoLinks(stage); Directory.CreateDirectory(stage);
            var seen = new HashSet<string>(StringComparer.OrdinalIgnoreCase); long total = 0;
            using (var input = ZipFile.OpenRead(zip)) {
                if (input.Entries.Count != Files.Length) throw new InvalidDataException("Архив содержит неполный или неизвестный набор файлов.");
                foreach (var entry in input.Entries) {
                    if (!Files.Contains(entry.FullName, StringComparer.Ordinal) || !seen.Add(entry.FullName) || (entry.ExternalAttributes & (int)FileAttributes.ReparsePoint) != 0 || ((entry.ExternalAttributes >> 16) & 0xf000) == 0xa000) throw new InvalidDataException("Небезопасный путь в архиве обновления.");
                    total += entry.Length;
                    if (entry.Length < 0 || entry.Length > 300L * 1024 * 1024 || total > 500L * 1024 * 1024) throw new InvalidDataException("Архив обновления слишком большой.");
                    string outputPath = Path.Combine(stage, entry.FullName); NoLinks(outputPath);
                    using (var source = entry.Open()) using (var output = new FileStream(outputPath, FileMode.CreateNew, FileAccess.Write, FileShare.None)) {
                        var buffer = new byte[65536]; long copied = 0; int count;
                        while ((count = source.Read(buffer, 0, buffer.Length)) != 0) { copied += count; if (copied > entry.Length) throw new InvalidDataException("Неверный размер файла в архиве."); output.Write(buffer, 0, count); }
                        if (copied != entry.Length) throw new InvalidDataException("Неполный файл в архиве."); output.Flush(true);
                    }
                }
            }
            if (Version.Parse(FileVersionInfo.GetVersionInfo(Path.Combine(stage, "Blizko.exe")).FileVersion) != release.Version) throw new InvalidDataException("Версия приложения не совпадает с описанием обновления.");
        }
        // The updater holds the application mutex. Only these twelve program files
        // can be replaced; the encrypted data directory is never included.
        internal static void Install(string zip, UpdateRelease release, string target, Action<int> fault = null) {
            target = Target(target); CheckWritable(target);
            if (File.Exists(Path.Combine(target, JournalName))) throw new IOException("Сначала необходимо завершить восстановление предыдущего обновления.");
            Version installed = Version.Parse(FileVersionInfo.GetVersionInfo(Path.Combine(target, "Blizko.exe")).FileVersion);
            if (release.Version <= installed) throw new InvalidDataException("Установлена эта или более новая версия.");
            string workName = ".blizko-update-" + Guid.NewGuid().ToString("N"), work = Path.Combine(target, workName);
            Directory.CreateDirectory(work); string stage = Path.Combine(work, "new"), backup = Path.Combine(work, "old");
            Directory.CreateDirectory(backup);
            bool committed = false;
            try {
                Extract(zip, stage, release);
                var journal = new UpdateJournal { format = 1, workspace = workName, files = new List<UpdateFile>() };
                foreach (string name in Files) {
                    string destination = Path.Combine(target, name); NoLinks(destination);
                    string oldHash = null;
                    if (File.Exists(destination)) { CopyDurable(destination, Path.Combine(backup, name)); oldHash = Hash(Path.Combine(backup, name)); }
                    journal.files.Add(new UpdateFile { name = name, oldHash = oldHash, newHash = Hash(Path.Combine(stage, name)) });
                }
                WriteAtomic(Path.Combine(target, JournalName), Encoding.UTF8.GetBytes(Json().Serialize(journal)));
                for (int i = 0; i < Files.Length; i++) {
                    string destination = Path.Combine(target, Files[i]); NoLinks(destination);
                    if (File.Exists(destination)) File.Replace(Path.Combine(stage, Files[i]), destination, null); else File.Move(Path.Combine(stage, Files[i]), destination);
                    if (fault != null) fault(i);
                }
                File.Delete(Path.Combine(target, JournalName)); committed = true;
            } catch {
                if (File.Exists(Path.Combine(target, JournalName))) Recover(target);
                throw;
            } finally {
                if (committed || !File.Exists(Path.Combine(target, JournalName))) TryCleanWorkspace(target, workName);
            }
        }
        internal static void Recover(string target) {
            target = Target(target); string marker = Path.Combine(target, JournalName); NoLinks(marker);
            if (!File.Exists(marker)) return;
            if (new FileInfo(marker).Length > 65536) throw new InvalidDataException("Повреждён журнал обновления.");
            var journal = Json().Deserialize<UpdateJournal>(File.ReadAllText(marker));
            if (journal == null || journal.format != 1 || !WorkspaceName(journal.workspace) || journal.files == null || journal.files.Count != Files.Length || !journal.files.Select(f => f.name).SequenceEqual(Files)) throw new InvalidDataException("Повреждён журнал обновления.");
            string backup = Path.Combine(target, journal.workspace, "old"); NoLinks(backup);
            // Validate every original before changing anything, also tolerating an
            // interrupted rollback whose original was already moved back.
            foreach (var file in journal.files) {
                if (!HashValid(file.newHash) || (file.oldHash != null && !HashValid(file.oldHash))) throw new InvalidDataException("Повреждён журнал обновления.");
                string original = Path.Combine(backup, file.name), destination = Path.Combine(target, file.name); NoLinks(original); NoLinks(destination);
                if (file.oldHash != null && !(File.Exists(original) && Hash(original) == file.oldHash) && !(File.Exists(destination) && Hash(destination) == file.oldHash)) throw new IOException("Не удалось проверить сохранённые файлы приложения. Папка восстановления сохранена.");
                if (file.oldHash == null && File.Exists(destination) && Hash(destination) != file.newHash) throw new IOException("Файлы приложения изменились во время обновления. Папка восстановления сохранена.");
            }
            foreach (var file in journal.files) {
                string original = Path.Combine(backup, file.name), destination = Path.Combine(target, file.name);
                if (file.oldHash == null) { if (File.Exists(destination)) File.Delete(destination); }
                else if (File.Exists(destination) && Hash(destination) == file.oldHash) { }
                else if (File.Exists(original)) { if (File.Exists(destination)) File.Replace(original, destination, null); else File.Move(original, destination); }
            }
            File.Delete(marker); TryCleanWorkspace(target, journal.workspace);
        }
        internal static bool WorkspaceName(string name) { return name != null && name.StartsWith(".blizko-update-", StringComparison.Ordinal) && name.Length == 47 && name.Substring(15).All(Uri.IsHexDigit); }
        private static void TryCleanWorkspace(string target, string name) { try { CleanWorkspace(target, name); } catch (IOException) { } catch (UnauthorizedAccessException) { } }
        internal static void CleanWorkspace(string target, string name) {
            if (!WorkspaceName(name)) throw new IOException("Неверная папка обновления.");
            string work = Path.Combine(target, name); NoLinks(work);
            if (!Directory.Exists(work)) return;
            foreach (string file in Directory.GetFileSystemEntries(work, "*", SearchOption.AllDirectories)) NoLinks(file);
            Directory.Delete(work, true);
        }
    }
}
