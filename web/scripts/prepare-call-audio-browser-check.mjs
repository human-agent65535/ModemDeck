import { cp, mkdir, readdir, readFile, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import path from 'node:path'
import ts from 'typescript'

// This script copies production assets; the page never requests a microphone,
// device, user session, WebSocket, push or call endpoint.
const web = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const output = process.argv[2] || '/private/tmp/modemdeck-production-neteq-web'
const names = await readdir(path.join(web, 'dist/assets'))
const workletCandidates = await Promise.all(names.filter(name => /^callAudio\.worklet-.*\.js$/.test(name))
  .map(async name => ({ name, source: await readFile(path.join(web, 'dist/assets', name), 'utf8') })))
// Vite also emits a tiny URL-export module with the same prefix. Load the
// registered processor itself, exactly as AudioWorklet.addModule does.
const worklet = workletCandidates.find(asset => asset.source.includes('registerProcessor') && asset.source.includes('modemdeck-call-audio'))?.name
const wasm = names.find(name => /^audioCore-.*\.wasm$/.test(name))
if (!worklet || !wasm) throw Error('Build production Web assets first')
await mkdir(output, { recursive: true })
await cp(path.join(web, 'dist/assets'), path.join(output, 'assets'), { recursive: true })
const protocol = await readFile(path.join(web, 'src/state/callAudioProtocol.ts'), 'utf8')
await writeFile(path.join(output, 'protocol.mjs'), ts.transpileModule(protocol, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } }).outputText)
await writeFile(path.join(output, 'index.html'), `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Production NetEq synthetic check</title><style>body{font:16px system-ui;max-width:65rem;margin:2rem auto;padding:1rem}button{font:inherit;padding:.6rem 1rem}pre{white-space:pre-wrap;overflow-wrap:anywhere}</style><h1>Production NetEq synthetic check</h1><p>Actual built AudioWorklet + shared Opus/NetEq WASM. Synthetic input only; no microphone, call, account or WebSocket. Offline rendering verifies execution and quality counters, not realtime deadlines.</p><button id="run">Run</button><pre id="result">Ready</pre><script type="module" src="/main.mjs"></script>`)
// Test-only wrapper: preserve the thrown processorerror, but report the
// constructor exception through the port obtained by the actual base owner.
await writeFile(path.join(output, 'diagnostic.worklet.mjs'), `const OriginalBase=globalThis.AudioWorkletProcessor;
const register=globalThis.registerProcessor;
let constructedPort;
globalThis.AudioWorkletProcessor=class extends OriginalBase{constructor(...args){super(...args);constructedPort=this.port}};
globalThis.registerProcessor=(name,Processor)=>register.call(globalThis,name,class extends Processor{
 constructor(...args){constructedPort=undefined;try{super(...args)}catch(error){constructedPort?.postMessage({type:'constructorerror',message:String(error),stack:error.stack,capabilities:{self:typeof self,location:typeof location,URL:typeof URL,console:typeof console,performance:typeof performance}});throw error}}
});`)
await writeFile(path.join(output, 'main.mjs'), `import { encodeCallAudioPacket, decodeCallAudioPacket } from './protocol.mjs';
const asset = ${JSON.stringify({ worklet: '/assets/' + worklet, wasm: '/assets/' + wasm })};
const output = document.querySelector('#result');
let constructorError;
function statistics(snapshot) {
 const d = new DataView(snapshot.buffer, snapshot.byteOffset, snapshot.byteLength), s = {};
 ['concealed_samples','concealment_events','inserted_samples','removed_samples','packets_discarded','packets_received','emitted_count','delay_ms_sum','target_delay_ms_sum'].forEach((name,i)=>s[name]=Number(d.getBigUint64(i*8,true)));
 ['target_delay_ms','current_delay_ms','internal_sample_rate','playout_timestamp48k','playout_timestamp_valid','frame_timestamp','speech_type'].forEach((name,i)=>s[name]=d.getUint32(72+i*4,true));
 s.real_output_samples=Number(d.getBigUint64(104,true));s.render_errors=Number(d.getBigUint64(120,true));s.ingress_queued=d.getUint32(128,true);return s;
}
async function runCase(name, delay, wrap, binary) {
 const context = new OfflineAudioContext(1, 12 * 48000, 48000);
 await context.audioWorklet.addModule('/diagnostic.worklet.mjs');
 await context.audioWorklet.addModule(asset.worklet);
 const node = new AudioWorkletNode(context,'modemdeck-call-audio',{numberOfInputs:1,numberOfOutputs:1,outputChannelCount:[1],processorOptions:{wasmBinary:binary}});
 const oscillator = new OscillatorNode(context,{frequency:440}), gain = new GainNode(context,{gain:.3});
 oscillator.connect(gain).connect(node).connect(context.destination);oscillator.start();
 let ready, encoded=0, received=0, previousArrival=0, stats, failure;
 const packets=[];
 node.onprocessorerror=()=>{failure??=Error('Actual production processorerror')};
 node.port.onmessage=({data})=>{
  if(data.type==='ready')ready=data;
  if(data.type==='error')failure=Error(data.message);
  if(data.type==='constructorerror'){constructorError=data;failure=Object.assign(Error(data.message),{stack:data.stack,capabilities:data.capabilities});output.textContent=JSON.stringify({status:'failed',constructorError},null,2);}
  if(data.type==='stats')stats=statistics(data.snapshot);
  if(data.type==='received')received++;
  if(data.type==='encoded'){
   node.port.postMessage({type:'ack',epoch:1});encoded++;
   const packet=decodeCallAudioPacket(encodeCallAudioPacket(data.sequence,data.payload));
   const batch=Math.max(0,Math.ceil(data.captureTime*10-1e-6)-1);
   previousArrival=Math.max(previousArrival,(batch+1)*100000+50000+delay(batch)*1000);
   packets.push({...packet,nowUs:previousArrival});
  }
 };
 node.port.postMessage({type:'activate',epoch:1,nowUs:0,contextTime:0,sequence:wrap?0xffffffce:0});
 const suspensions=[];
 for(let step=1;step<120;step++){
  suspensions.push(context.suspend(step/10).then(()=>{
   const nowUs=context.currentTime*1e6;
   while(packets.length&&packets[0].nowUs<=nowUs){const packet=packets.shift();node.port.postMessage({type:'packet',epoch:1,...packet},[packet.payload.buffer]);}
   if(step===119)node.port.postMessage({type:'stats',epoch:1});
   return context.resume();
  }));
 }
 const started=performance.now();
 const buffer=await Promise.race([context.startRendering(),new Promise((_,reject)=>setTimeout(()=>reject(Error('Offline production render timed out')),30000))]);
 await Promise.all(suspensions);
 await new Promise((resolve,reject)=>{const timeout=setTimeout(()=>reject(Error('Production stats not delivered')),5000);const done=()=>{if(failure){clearTimeout(timeout);reject(failure)}else if(stats){clearTimeout(timeout);resolve()}else requestAnimationFrame(done)};done()});
 if(failure)throw failure;if(!ready||!encoded||!received||stats.render_errors)throw Error('Incomplete actual audio pipeline');
 const pcm=buffer.getChannelData(0);let checksum=2166136261,energy=0,zeros=0;
 for(let i=0;i<pcm.length;i++){const value=Math.round(pcm[i]*32768);checksum=Math.imul(checksum^(value&65535),16777619)>>>0;if(i>=8*48000&&i<10*48000){energy+=pcm[i]*pcm[i];if(!pcm[i])zeros++}}
 node.port.postMessage({type:'deactivate',epoch:2});node.disconnect();oscillator.disconnect();gain.disconnect();
 return {name,asset,ready,encoded,received,checksum,tailEnergy:energy,tailZeroSamples:zeros,stats,offlineWallMs:performance.now()-started};
}
document.querySelector('#run').onclick=async()=>{
 const button=document.querySelector('#run');button.disabled=true;output.textContent='Running production synthetic audio…';
 try{const response=await fetch(asset.wasm);if(!response.ok)throw Error('WASM fetch '+response.status);const binary=await response.arrayBuffer();const results=[];
 for(const [name,delay,wrap]of[['baseline100',()=>0,false],['single100',b=>b===4?100:0,false],['alternating100',b=>b%2?100:0,false],['wrap',()=>0,true]]){results.push(await runCase(name,delay,wrap,binary));output.textContent=JSON.stringify({status:'running',results},null,2)}
 const pass=results.every(r=>r.tailEnergy>1000&&r.stats.internal_sample_rate===48000&&r.stats.render_errors===0);
 output.textContent=JSON.stringify({status:pass?'passed':'failed',limits:'Offline execution does not establish realtime deadlines or real-device acoustics',results},null,2);
 }catch(error){output.textContent=JSON.stringify({status:'failed',message:String(error),stack:error.stack,capabilities:error.capabilities,constructorError},null,2)}finally{button.disabled=false}
};`)
console.log(`Production synthetic browser page prepared at ${output}; serve this directory on localhost. No service started.`)
