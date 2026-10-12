import test from 'node:test';
import assert from 'node:assert/strict';
import {Bridge,panelSize,surfaceLayout} from './app.mjs';

test('inline sizing does not take the current iframe height as content height',()=>{
  assert.deepEqual(surfaceLayout(),{kind:'content',displayMode:'inline',height:null,maxHeight:null});
  assert.equal(surfaceLayout({displayMode:'fullscreen'}).kind,'viewport');
  assert.equal(surfaceLayout({displayMode:'inline',containerDimensions:{height:480,width:390}}).kind,'fixed');
  assert.deepEqual(surfaceLayout({containerDimensions:{maxHeight:640,maxWidth:390}}),{kind:'content',displayMode:'inline',height:null,maxHeight:640});
  for(const height of [0,-1,NaN,Infinity,'600'])assert.equal(surfaceLayout({containerDimensions:{height}}).kind,'content');
  assert.deepEqual(panelSize({height:870.773},801),{height:871,width:801});
  assert.equal(panelSize({height:0},390),null);
});

test('an autosize acknowledgment cannot restart document-scroll-height feedback',async t=>{
  const keys=['window','document','ResizeObserver'],previous=new Map(keys.map(k=>[k,Object.getOwnPropertyDescriptor(globalThis,k)]));
  t.after(()=>{for(const k of keys){const value=previous.get(k);if(value)Object.defineProperty(globalThis,k,value);else delete globalThis[k];}});
  const listeners=new Map(),frames=[],reports=[],styles=new Map();let observer,bodyHeight=870.773;
  const root={dataset:{},clientWidth:801,clientHeight:600,scrollHeight:871,style:{setProperty:(k,v)=>styles.set(k,v),removeProperty:k=>styles.delete(k)}};
  const parent={postMessage(message){if(message.method==='ui/notifications/size-changed'){reports.push(message.params);root.clientHeight=message.params.height;root.scrollHeight=message.params.height+48;observer.callback();}else if(message.id!=null)queueMicrotask(()=>listeners.get('message')({source:parent,data:{jsonrpc:'2.0',id:message.id,result:message.method==='ui/initialize'?{hostContext:{displayMode:'inline',theme:'dark'}}:{isError:false}}}));}};
  globalThis.window={parent,addEventListener:(name,fn)=>listeners.set(name,fn),requestAnimationFrame:fn=>(frames.push(fn),frames.length)};
  globalThis.document={hidden:false,documentElement:root,body:{getBoundingClientRect:()=>({height:bodyHeight})},addEventListener:(name,fn)=>listeners.set(name,fn)};
  globalThis.ResizeObserver=class{constructor(callback){this.callback=callback;observer=this;}observe(){}};
  const bridge=new Bridge();await bridge.initialize();
  for(let i=0;i<8&&frames.length;i++)frames.shift()();
  assert.deepEqual(reports,[{height:871,width:801}]);
  assert.equal(root.scrollHeight,919,'the host can report overflowing document height without it becoming a UI measurement');
  assert.equal(frames.length,0);
  // A real width/mode change must still be observed and should clear obsolete
  // fixed dimensions rather than preserve a previous display mode's height.
  bridge.context({displayMode:'fullscreen',containerDimensions:{height:480,width:390}});
  root.clientWidth=390;bodyHeight=480;frames.shift()();
  assert.equal(root.dataset.surfaceLayout,'fixed');assert.equal(styles.get('--codexm-host-height'),'480px');
  assert.deepEqual(reports.at(-1),{height:480,width:390});
  bridge.context({displayMode:'inline',containerDimensions:{maxHeight:640,maxWidth:390}});
  assert.equal(root.dataset.surfaceLayout,'content');assert.equal(styles.has('--codexm-host-height'),false);
  assert.equal(styles.get('--codexm-host-max-height'),'640px');
  while(frames.length)frames.shift()();
  const inputs=[];bridge.onInput=payload=>inputs.push(payload);
  for(const method of ['ui/notifications/tool-input','ui/notifications/partial-tool-input'])bridge.receive({source:parent,data:{jsonrpc:'2.0',method,params:{arguments:{initial_view:'accounts'}}}});
  assert.equal(inputs.length,2);
  await new Promise(resolve=>setImmediate(resolve));
});
