using System;
using System.Diagnostics;
using System.IO;
using System.Threading;
using System.Windows.Forms;

namespace Blizko {
    internal static class UpdaterProgram {
        [STAThread] private static int Main(string[] args) {
            UpdatePlan plan = null; bool parentExited = false;
            try {
                Directory.SetCurrentDirectory(AppDomain.CurrentDomain.BaseDirectory);
                if (args.Length != 1) throw new IOException("Откройте обновления из приложения «Близко».");
                string planPath = Path.GetFullPath(args[0]); UpdatePackage.NoLinks(planPath);
                if (Path.GetDirectoryName(planPath) != AppDomain.CurrentDomain.BaseDirectory.TrimEnd('\\') || new FileInfo(planPath).Length > 65536) throw new IOException("Неверный план обновления.");
                plan = UpdatePackage.Json().Deserialize<UpdatePlan>(File.ReadAllText(planPath));
                if (plan == null || (plan.action != "install" && plan.action != "recover")) throw new IOException("Неверный план обновления.");
                plan.target = UpdatePackage.Target(plan.target);
                UpdateRelease release = null;
                if (plan.action == "install") { if(new FileInfo("manifest.json").Length>65536)throw new IOException("Описание обновления слишком большое.");release = UpdatePackage.Verify(File.ReadAllText("manifest.json")); UpdatePackage.VerifyArchive("package.zip", release); }
                using (var parent = Process.GetProcessById(plan.parent)) {
                    if (parent.StartTime.ToUniversalTime().Ticks != plan.parentStarted || !String.Equals(parent.MainModule.FileName, Path.Combine(plan.target, "Blizko.exe"), StringComparison.OrdinalIgnoreCase)) throw new IOException("Приложение, запросившее обновление, изменилось.");
                    UpdatePackage.WriteAtomic("ready", new byte[] { 1 });
                    if (!parent.WaitForExit(60000)) throw new IOException("Приложение не закрылось. Обновление отменено."); parentExited = true;
                }
                using (var mutex = new Mutex(false, "Local\\Blizko.Windows.UI")) {
                    bool locked = false;
                    try {
                        try { locked = mutex.WaitOne(30000); } catch (AbandonedMutexException) { locked = true; }
                        if (!locked) throw new IOException("Близко запущено в другом окне. Обновление отменено.");
                        if (plan.action == "recover") UpdatePackage.Recover(plan.target); else UpdatePackage.Install("package.zip", release, plan.target);
                    } finally { if (locked) mutex.ReleaseMutex(); }
                }
                Launch(plan); return 0;
            } catch (Exception e) {
                try { File.WriteAllText("update-error.txt", e.ToString()); } catch (Exception) { }
                MessageBox.Show("Обновление не установлено. " + e.Message + "\n\nПереписка хранится отдельно от файлов приложения.", "Близко · обновление", MessageBoxButtons.OK, MessageBoxIcon.Information);
                if (parentExited && plan != null && !File.Exists(Path.Combine(plan.target, UpdatePackage.JournalName))) { try { Launch(plan); } catch (Exception) { } }
                return 1;
            }
        }
        private static void Launch(UpdatePlan plan) { Process.Start(new ProcessStartInfo { FileName = Path.Combine(plan.target, "Blizko.exe"), Arguments = plan.receive ? "" : "--receive-off", WorkingDirectory = plan.target, UseShellExecute = false }); }
    }
}
