using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;
using System.Web.Script.Serialization;
class FakeEngine {
 static readonly object Output=new object();
 static void Main(){string raw;var workers=new List<Task>();while((raw=Console.ReadLine())!=null){var request=new JavaScriptSerializer().Deserialize<Dictionary<string,object>>(raw);workers.Add(Task.Run(()=>{
  string op=(string)request["op"];if(op=="send"||op=="check")Thread.Sleep(600);
  lock(Output)Console.WriteLine(new JavaScriptSerializer().Serialize(new {id=request["id"],code="fixture probe",snapshot=new{status="fixture",enabled=false,online=false,incoming=0,revision=1}}));
 }));}Task.WaitAll(workers.ToArray());}
}
