using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using System.Web.Script.Serialization;

namespace Blizko {
    public sealed class Contact {
        public string id { get; set; }
        public string name { get; set; }
        public string address { get; set; }
        public string Preview { get; set; }
        public string Initial { get { return String.IsNullOrWhiteSpace(name) ? "?" : name.Substring(0, 1).ToUpperInvariant(); } }
    }
    public sealed class ChatMessage {
        public string id { get; set; }
        public string peer { get; set; }
        public string text { get; set; }
        public bool @out { get; set; }
        public bool delivered { get; set; }
        public long time { get; set; }
        public long order { get; set; }
    }
    public sealed class Snapshot {
        public string id { get; set; }
        public string status { get; set; }
        public bool enabled { get; set; }
        public bool online { get; set; }
        public bool relayOnly { get; set; }
        public int incoming { get; set; }
        public long revision { get; set; }
        public bool hasMore { get; set; }
        public Dictionary<string, string> previews { get; set; }
        public List<Contact> contacts { get; set; }
        public List<ChatMessage> messages { get; set; }
        public Dictionary<string, string> deliveryIssues { get; set; }
        public Snapshot() { contacts = new List<Contact>(); messages = new List<ChatMessage>(); status = "Открываем хранилище…"; }
    }
    public sealed class Reply {
        public int id { get; set; }
        public string error { get; set; }
        public string code { get; set; }
        public Snapshot snapshot { get; set; }
    }
    public sealed class Engine : IDisposable {
        private readonly Process process;
        private readonly StreamWriter input;
        private readonly SemaphoreSlim serial = new SemaphoreSlim(1, 1);
        private readonly JavaScriptSerializer json = new JavaScriptSerializer { MaxJsonLength = 32 * 1024 * 1024 };
        private int sequence;
        private bool disposed;
        public Engine(string directory) {
            string executable = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "Blizko.Engine.exe");
            process = new Process { StartInfo = new ProcessStartInfo {
                FileName = executable, Arguments = "--data-dir " + Quote(directory),
                WorkingDirectory = AppDomain.CurrentDomain.BaseDirectory, UseShellExecute = false,
                CreateNoWindow = true, RedirectStandardInput = true, RedirectStandardOutput = true,
                RedirectStandardError = true, StandardOutputEncoding = Encoding.UTF8, StandardErrorEncoding = Encoding.UTF8
            }};
            process.Start();
            input = new StreamWriter(process.StandardInput.BaseStream, new UTF8Encoding(false)) { AutoFlush = true };
            // Consume diagnostics so a full stderr pipe never blocks the engine.
            process.ErrorDataReceived += delegate { };
            process.BeginErrorReadLine();
        }
        private static string Quote(string argument) {
            var result = new StringBuilder("\""); int slashes = 0;
            foreach (char value in argument) {
                if (value == '\\') { slashes++; continue; }
                if (value == '"') { result.Append('\\', slashes * 2 + 1).Append('"'); slashes = 0; continue; }
                result.Append('\\', slashes).Append(value); slashes = 0;
            }
            return result.Append('\\', slashes * 2).Append('"').ToString();
        }
        public async Task<Reply> Request(string op, string first = "", string second = "", bool value = false, long before = 0, int limit = 50) {
            await serial.WaitAsync();
            try {
                if (disposed || process.HasExited) throw new IOException("Ядро остановлено. Откройте приложение заново; история сохранена.");
                int id = ++sequence;
                string request = json.Serialize(new { id = id, op = op, first = first, second = second, value = value, before = before, limit = limit });
                await input.WriteLineAsync(request);
                Task<string> read = process.StandardOutput.ReadLineAsync();
                if (await Task.WhenAny(read, Task.Delay(40000)) != read) {
                    if (!process.HasExited) process.Kill();
                    throw new IOException("Соединение не отвечает. Откройте приложение заново; история сохранена.");
                }
                string raw = await read;
                if (raw == null || raw.Length > json.MaxJsonLength) throw new IOException("Ядро остановлено. История сохранена.");
                Reply reply = json.Deserialize<Reply>(raw);
                if (!String.IsNullOrEmpty(reply.error)) throw new IOException(reply.error);
                if (reply.id != id) throw new IOException("Не удалось проверить ответ приложения.");
                return reply;
            } finally { serial.Release(); }
        }
        public void Dispose() {
            if (disposed) return; disposed = true;
            try { input.Close(); if (!process.WaitForExit(3000)) process.Kill(); } catch (InvalidOperationException) { }
            process.Dispose();
        }
    }
}
