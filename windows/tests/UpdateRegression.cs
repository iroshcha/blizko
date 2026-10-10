using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.IO.Compression;
using System.Linq;
using System.Security.Cryptography;
using System.Text;
using System.Threading;
using Blizko;

class UpdateRegression {
    static string root;
    const string Config="<?xml version=\"1.0\"?><configuration><startup><supportedRuntime version=\"v4.0\"/></startup></configuration>";
    static void Assert(bool condition,string reason){if(!condition)throw new Exception(reason);}
    static void Reject(Action action,string reason){try{action();}catch(Exception){return;}throw new Exception("Accepted: "+reason);}
    static string Target(string name){string path=Path.Combine(root,name);Directory.CreateDirectory(path);foreach(string file in UpdatePackage.Files){if(file=="Blizko.Updater.exe"||file=="Blizko.Updater.exe.config")continue;if(file=="Blizko.exe")File.Copy(Path.Combine(root,"old.exe"),Path.Combine(path,file));else File.WriteAllText(Path.Combine(path,file),file=="Blizko.exe.config"?Config:"old:"+file);}File.WriteAllText(Path.Combine(path,"keep.txt"),"user file");Directory.CreateDirectory(Path.Combine(path,"data"));File.WriteAllText(Path.Combine(path,"data","history.enc"),"encrypted-history-fixture");return path;}
    static void Old(string target){foreach(string file in UpdatePackage.Files){string path=Path.Combine(target,file);if(file.StartsWith("Blizko.Updater"))Assert(!File.Exists(path),"new helper survived rollback");else if(file=="Blizko.exe")Assert(UpdatePackage.Hash(path)==UpdatePackage.Hash(Path.Combine(root,"old.exe")),"original exe not restored");else Assert(File.ReadAllText(path)==(file=="Blizko.exe.config"?Config:"old:"+file),"original not restored: "+file);}Data(target);Assert(!File.Exists(Path.Combine(target,UpdatePackage.JournalName)),"journal survived rollback");}
    static void Data(string target){Assert(File.ReadAllText(Path.Combine(target,"keep.txt"))=="user file","unrelated file changed");Assert(File.ReadAllText(Path.Combine(target,"data","history.enc"))=="encrypted-history-fixture","history changed");}
    static string Zip(string name,string replacement=null,bool duplicate=false){string path=Path.Combine(root,name+".zip");using(var zip=ZipFile.Open(path,ZipArchiveMode.Create)){foreach(string file in UpdatePackage.Files){string entry=file=="README.txt"&&replacement!=null?replacement:file;using(var stream=zip.CreateEntry(entry).Open()){if(file=="Blizko.exe"){byte[] bytes=File.ReadAllBytes(Path.Combine(root,"new.exe"));stream.Write(bytes,0,bytes.Length);}else{byte[] bytes=Encoding.UTF8.GetBytes(file.EndsWith(".config")?Config:"new:"+file);stream.Write(bytes,0,bytes.Length);}}}if(duplicate)zip.CreateEntry("Blizko.exe");}return path;}
    static UpdateRelease Release(string zip){return new UpdateRelease{version="0.2.5.0",url="https://github.com/iroshcha/blizko/releases/download/test/Blizko-Windows-x64.zip",sha256=UpdatePackage.Hash(zip),size=new FileInfo(zip).Length,notes="Тест"};}
    static string Sign(UpdateRelease release){using(var rsa=new RSACryptoServiceProvider()){rsa.PersistKeyInCsp=false;rsa.FromXmlString(File.ReadAllText(Path.Combine(root,"test-private.xml")));byte[] payload=Encoding.UTF8.GetBytes(UpdatePackage.Json().Serialize(release));return UpdatePackage.Json().Serialize(new SignedUpdate{payload=Convert.ToBase64String(payload),signature=Convert.ToBase64String(rsa.SignData(payload,CryptoConfig.MapNameToOID("SHA256")))});}}
    static int Main(string[] args){try{
        Console.OutputEncoding=new UTF8Encoding(false);root=Path.GetFullPath(args[0]);
        if(args.Length>1&&args[1]=="crash"){string zip=Path.Combine(root,"valid.zip");UpdatePackage.Install(zip,Release(zip),args[2],i=>{if(i==7)Environment.Exit(17);});return 2;}
        string valid=Zip("valid");var release=Release(valid);string signed=Sign(release);
        Assert(UpdatePackage.Verify(signed).version=="0.2.5.0","signed manifest not accepted");
        var envelope=UpdatePackage.Json().Deserialize<SignedUpdate>(signed);envelope.payload=Convert.ToBase64String(Encoding.UTF8.GetBytes("{}"));Reject(()=>UpdatePackage.Verify(UpdatePackage.Json().Serialize(envelope)),"modified signed payload");
        using(var other=new RSACryptoServiceProvider()){Reject(()=>UpdatePackage.Verify(signed,other.ToXmlString(false)),"wrong signing key");}
        release.url="http://github.com/iroshcha/blizko/releases/download/test/Blizko-Windows-x64.zip";Reject(()=>UpdatePackage.Verify(Sign(release)),"HTTP update");release=Release(valid);
        release.url="https://example.com/Blizko-Windows-x64.zip";Reject(()=>UpdatePackage.Verify(Sign(release)),"foreign download host");release=Release(valid);
        Console.WriteLine("PASS: valid signature, tampering, wrong key and unsafe URLs");
        string damaged=Path.Combine(root,"damaged.zip");File.Copy(valid,damaged);using(var f=new FileStream(damaged,FileMode.Open,FileAccess.Write)){f.Position=20;f.WriteByte(123);}Reject(()=>UpdatePackage.VerifyArchive(damaged,release),"corrupt archive");
        string traversal=Zip("traversal","../escaped.txt");Reject(()=>UpdatePackage.Extract(traversal,Path.Combine(root,"unsafe"),Release(traversal)),"ZIP path traversal");Assert(!File.Exists(Path.Combine(root,"escaped.txt")),"escaped ZIP path written");
        string duplicate=Zip("duplicate",null,true);Reject(()=>UpdatePackage.Extract(duplicate,Path.Combine(root,"duplicate"),Release(duplicate)),"duplicate ZIP entry");
        string missing=Zip("unknown","unknown.dll");Reject(()=>UpdatePackage.Extract(missing,Path.Combine(root,"unknown"),Release(missing)),"unknown package file");
        Console.WriteLine("PASS: damaged archive, traversal, duplicates and unknown files rejected");
        foreach(int fail in new[]{0,6,11}){string target=Target("rollback-"+fail);Reject(()=>UpdatePackage.Install(valid,release,target,i=>{if(i==fail)throw new IOException("Injected replacement failure");}),"injected failure");Old(target);}
        string locked=Target("locked");using(var file=new FileStream(Path.Combine(locked,"Blizko.exe"),FileMode.Open,FileAccess.Read,FileShare.Read)){Reject(()=>UpdatePackage.Install(valid,release,locked),"locked executable");}Old(locked);
        Console.WriteLine("PASS: rollback after early/middle/last replacement and locked executable; user files preserved");
        string crash=Target("crash");var child=Process.Start(new ProcessStartInfo{FileName=typeof(UpdateRegression).Assembly.Location,Arguments=WindowsUpdates.Quote(root)+" crash "+WindowsUpdates.Quote(crash),UseShellExecute=false,CreateNoWindow=true});Assert(child.WaitForExit(30000)&&child.ExitCode==17,"crash fixture failed");Assert(File.Exists(Path.Combine(crash,UpdatePackage.JournalName)),"crash did not retain journal");UpdatePackage.Recover(crash);Old(crash);
        Console.WriteLine("PASS: process interruption leaves recoverable journal and restores old files");
        string installed=Target("installed");UpdatePackage.Install(valid,release,installed);Assert(UpdatePackage.Hash(Path.Combine(installed,"Blizko.exe"))==UpdatePackage.Hash(Path.Combine(root,"new.exe")),"new exe not installed");Data(installed);Reject(()=>UpdatePackage.Install(valid,release,installed),"same-version reinstall");
        Console.WriteLine("PASS: complete installation, unchanged history and downgrade/reinstall guard");
        // Exercise the real updater program with an isolated parent and restart fixture.
        string end=Target("end-to-end"),work=Path.Combine(root,"handoff");Directory.CreateDirectory(work);
        foreach(string file in new[]{"Blizko.Updater.exe","Blizko.Updater.exe.config"})File.Copy(Path.Combine(root,file),Path.Combine(work,file));
        File.Copy(valid,Path.Combine(work,"package.zip"));File.WriteAllText(Path.Combine(work,"manifest.json"),signed);
        var parent=Process.Start(new ProcessStartInfo{FileName=Path.Combine(end,"Blizko.exe"),Arguments=WindowsUpdates.Quote(work),WorkingDirectory=end,UseShellExecute=false,CreateNoWindow=true});
        Assert(parent.WaitForExit(30000)&&parent.ExitCode==0,"parent/helper handshake failed");
        var until=DateTime.UtcNow.AddSeconds(30);string marker=Path.Combine(end,"restarted.txt");
        while(!File.Exists(marker)&&DateTime.UtcNow<until&&!File.Exists(Path.Combine(work,"update-error.txt")))Thread.Sleep(100);
        if(!File.Exists(marker)){string error=File.Exists(Path.Combine(work,"update-error.txt"))?File.ReadAllText(Path.Combine(work,"update-error.txt")):"restart timeout";try{var helper=Process.GetProcessById(Int32.Parse(File.ReadAllText(Path.Combine(work,"helper.pid"))));if(String.Equals(helper.MainModule.FileName,Path.Combine(work,"Blizko.Updater.exe"),StringComparison.OrdinalIgnoreCase))helper.Kill();}catch(Exception){}throw new Exception(error);}
        Assert(File.ReadAllText(marker)=="--receive-off","disabled receive not preserved");Data(end);
        Console.WriteLine("PASS: real updater handshake, parent exit, verified replacement, restart and receive preference");return 0;
    }catch(Exception e){Console.Error.WriteLine(e);return 1;}}
}
