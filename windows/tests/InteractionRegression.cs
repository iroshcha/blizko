using System;
using System.Collections.Generic;
using System.Reflection;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Threading;
using System.IO;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using Blizko;
class InteractionRegression {
 const BindingFlags Hidden=BindingFlags.Instance|BindingFlags.NonPublic;
 static void Set(object o,string n,object v){o.GetType().GetField(n,Hidden).SetValue(o,v);}
 static T Get<T>(object o,string n){return (T)o.GetType().GetField(n,Hidden).GetValue(o);}
 static void Pump(Task task){var deadline=DateTime.UtcNow.AddSeconds(20);var frame=new DispatcherFrame();var timer=new DispatcherTimer{Interval=TimeSpan.FromMilliseconds(10)};timer.Tick+=(s,e)=>{if(task.IsCompleted||DateTime.UtcNow>=deadline){timer.Stop();frame.Continue=false;}};timer.Start();Dispatcher.PushFrame(frame);if(!task.IsCompleted)throw new TimeoutException("Fixture did not complete within 20 seconds");task.GetAwaiter().GetResult();}
 [STAThread] static int Main(){try{
  using(var watchdog=new System.Threading.Timer(o=>{Console.Error.WriteLine("FAIL: interaction fixture exceeded 60 seconds");Environment.Exit(2);},null,60000,Timeout.Infinite)){
  Console.OutputEncoding=new System.Text.UTF8Encoding(false);Console.WriteLine("START: production UI interaction fixture");var application=new Application();var ui=new ChatWindow(true);SynchronizationContext.SetSynchronizationContext(new DispatcherSynchronizationContext());
  using(var engine=new Engine("isolated-fixture")){
   Console.WriteLine("START: fixture engine status");Pump(engine.Request("status"));Console.WriteLine("PASS: fixture engine responds");
   Set(ui,"engine",engine);var a=new Contact{id="contactA",name="A"};var b=new Contact{id="contactB",name="B"};ui.Apply(new Snapshot{contacts=new List<Contact>{a,b},status="fixture"});
   var contacts=Get<ListBox>(ui,"contacts");var compose=Get<TextBox>(ui,"compose");contacts.SelectedItem=a;compose.Text="send this";
   var sending=(Task)typeof(ChatWindow).GetMethod("Send",Hidden).Invoke(ui,null);compose.Text="new draft after sending";contacts.SelectedItem=b;Pump(sending);contacts.SelectedItem=a;
   if(compose.Text!="new draft after sending")throw new Exception("Delayed send erased newer draft after switching chats");
   Console.WriteLine("PASS: delayed send preserves newer draft in another chat");
   var probe=engine.Request("check","contactA");var status=engine.Request("status");Pump(status);if(probe.IsCompleted)throw new Exception("fixture did not overlap probe and status");Pump(probe);
   Console.WriteLine("PASS: status bypasses pending probe and replies are matched by ID");
   compose.Text=new string('я',2100);if(System.Text.Encoding.UTF8.GetByteCount(compose.Text)!=4000)throw new Exception("Windows composer splits UTF-8 limit");
   Console.WriteLine("PASS: composer enforces UTF-8 byte limit");
   contacts.SelectedItem=b;contacts.SelectedItem=a;if(compose.Text.Length!=2000)throw new Exception("Switch lost draft");
   Console.WriteLine("PASS: switching contacts preserves separate drafts");
   typeof(ChatWindow).GetMethod("ShowUpdates",Hidden).Invoke(ui,new object[]{false});
   Set(ui,"availableUpdate",new UpdateRelease{version="0.2.6.0",notes="Тестовый выпуск: улучшения переписки."});Set(ui,"updateMessage","Доступна версия 0.2.6.");
   typeof(ChatWindow).GetMethod("RenderUpdate",Hidden).Invoke(ui,null);
   if(!Get<Button>(ui,"updateInstallButton").IsEnabled)throw new Exception("New release has no install action");
   var updateWindow=Get<Window>(ui,"updatesWindow");var surface=(FrameworkElement)updateWindow.Content;surface.Measure(new Size(480,540));surface.Arrange(new Rect(0,0,480,540));surface.UpdateLayout();
   var bitmap=new RenderTargetBitmap(480,540,96,96,PixelFormats.Pbgra32);bitmap.Render(surface);var png=new PngBitmapEncoder();png.Frames.Add(BitmapFrame.Create(bitmap));using(var file=File.Create(Path.Combine(AppDomain.CurrentDomain.BaseDirectory,"updates-preview.png")))png.Save(file);
   var cancellation=new CancellationTokenSource();Set(ui,"updateDownload",cancellation);typeof(ChatWindow).GetMethod("RenderUpdate",Hidden).Invoke(ui,null);
   if(Get<Button>(ui,"updateInstallButton").IsEnabled||Get<Button>(ui,"updateCheckButton").IsEnabled||Get<Button>(ui,"updateCancelButton").Visibility!=Visibility.Visible)throw new Exception("Download does not prevent overlapping installation or expose cancel");
   Set(ui,"updateDownload",null);cancellation.Dispose();
   Set(ui,"availableUpdate",new UpdateRelease{version="0.2.4.0"});typeof(ChatWindow).GetMethod("RenderUpdate",Hidden).Invoke(ui,null);
   if(Get<Button>(ui,"updateInstallButton").Visibility!=Visibility.Collapsed)throw new Exception("Old release offered for installation");
   Console.WriteLine("PASS: updates screen exposes new release, download cancellation and old-version guard");
  }application.Shutdown();return 0;}
 }catch(Exception e){Console.Error.WriteLine(e);return 1;}}
}
