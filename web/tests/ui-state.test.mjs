import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createUIStore,connectionView,deliveryView,composerView} from '../src/ui-state.js';

test('late connection checks cannot overwrite a newly selected conversation',()=>{
 const ui=createUIStore();ui.dispatch({type:'select',peer:'a'});const selection=ui.state.selection;
 ui.dispatch({type:'probe',peer:'a',selection,status:'checking'});
 ui.dispatch({type:'select',peer:'b'});ui.dispatch({type:'select',peer:'a'});
 ui.dispatch({type:'probe',peer:'a',selection,status:'success',text:'old result'});
 assert.equal(ui.state.probe,null);
});
test('switching conversations does not unlock a pending send or leak its error',()=>{
 const ui=createUIStore();ui.dispatch({type:'select',peer:'a'});ui.dispatch({type:'send-start'});
 ui.dispatch({type:'select',peer:'b'});assert.equal(ui.state.sending,true);
 ui.dispatch({type:'send-end',peer:'a',error:'disk full'});
 assert.equal(ui.state.sending,false);assert.equal(ui.state.sendError,'');
});
test('offline presentation cannot claim sending or erase confirmed delivery',()=>{
 assert.equal(connectionView('online',false).kind,'offline');
 assert.equal(connectionView('connecting').kind,'connecting');
 const context={online:false,sending:true,issue:'offline'};
 assert.equal(deliveryView({delivered:false},context).kind,'queued');
 assert.equal(deliveryView({delivered:true},context).kind,'delivered');
 assert.equal(deliveryView({cancelled:true},context).kind,'cancelled');
 assert.equal(deliveryView({storedUntil:Date.now()+60000},context).kind,'stored');
 assert.equal(deliveryView({storedUntil:Date.now()-1},context).kind,'queued');
});
test('composer uses wire byte limit and prevents empty or duplicate submissions',()=>{
 assert.equal(composerView(' \n ',false).disabled,true);
 assert.equal(composerView('Привет',true).disabled,true);
 assert.equal(composerView('я'.repeat(2000),false).disabled,false);
 assert.equal(composerView('я'.repeat(2001),false).disabled,true);
 assert.equal(composerView('👋'.repeat(1000),false).disabled,false);
});
