using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Threading;
using System.Web.Script.Serialization;
#if OLD
[assembly: AssemblyVersion("0.2.4.0")]
[assembly: AssemblyFileVersion("0.2.4.0")]
#else
[assembly: AssemblyVersion("0.2.5.0")]
[assembly: AssemblyFileVersion("0.2.5.0")]
#endif
class UpdateFixture {
    static int Main(string[] args) {
#if OLD
        string work = args[0], target = AppDomain.CurrentDomain.BaseDirectory.TrimEnd('\\');
        using (var parent = Process.GetCurrentProcess()) {
            var plan = new { action = "install", target = target, parent = parent.Id, parentStarted = parent.StartTime.ToUniversalTime().Ticks, receive = false };
            File.WriteAllText(Path.Combine(work, "plan.json"), new JavaScriptSerializer().Serialize(plan));
        }
        var helper = Process.Start(new ProcessStartInfo { FileName = Path.Combine(work,"Blizko.Updater.exe"), Arguments = "\"" + Path.Combine(work,"plan.json") + "\"", WorkingDirectory = work, UseShellExecute = false, CreateNoWindow = true });
        File.WriteAllText(Path.Combine(work,"helper.pid"), helper.Id.ToString());
        var until=DateTime.UtcNow.AddSeconds(20);
        while(DateTime.UtcNow<until){if(helper.HasExited)return 3;if(File.Exists(Path.Combine(work,"ready")))return 0;Thread.Sleep(50);}
        helper.Kill();return 4;
#else
        File.WriteAllText(Path.Combine(AppDomain.CurrentDomain.BaseDirectory,"restarted.txt"),String.Join(" ",args)); return 0;
#endif
    }
}
