'use strict';
const $=id=>document.getElementById(id);
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const fmtN=n=>new Intl.NumberFormat('en-US').format(Number(n||0));
const short=(s,n=10)=>{s=String(s||'');return s?s.slice(0,n):'—'};

const JOURNEY=[
  {id:'INBOUND',title:'LOADED INBOUND',copy:'Full truck enters from Side A.'},
  {id:'PASS1',title:'PASS 1 · GROSS',copy:'Loaded truck is positioned and gross weight is captured.'},
  {id:'UNLOAD',title:'UNLOAD AT WAREHOUSE',copy:'Truck leaves the scale, queues, then dumps material.'},
  {id:'RETURN',title:'EMPTY RETURN',copy:'The exact called truck returns from Side B.'},
  {id:'PASS2',title:'PASS 2 · TARE',copy:'Empty truck is positioned and tare weight is captured.'},
  {id:'COMPLETE',title:'NET COMPLETE',copy:'Gross − tare is durably committed as net material.'}
];

let live=true,demoTimer=null,demoIndex=0,lastCamera='';
let data={health:null,wf:null,scale:null,io:null,id:null,storage:null,queue:[],weights:[],events:[],latest:null,central:null,demo:null};

async function getJSON(url){const r=await fetch(url,{cache:'no-store'});if(!r.ok)throw new Error(`${url} ${r.status}`);return r.json()}
async function postJSON(url){const r=await fetch(url,{method:'POST',headers:{'Content-Type':'application/json'}});if(!r.ok)throw new Error(`${url} ${r.status}: ${await r.text()}`);return r.json()}

async function pollFast(){
  if(!live)return;
  const urls=['/healthz','/api/workflow','/api/scale/status','/api/io/status','/api/identity','/api/storage/status','/api/queue','/api/tickets/latest','/api/central/status'];
  const res=await Promise.allSettled(urls.map(getJSON));
  ['health','wf','scale','io','id','storage'].forEach((k,i)=>{if(res[i].status==='fulfilled')data[k]=res[i].value});
  if(res[6].status==='fulfilled')data.queue=res[6].value.items||[];
  if(res[7].status==='fulfilled')data.latest=res[7].value.ticket||res[7].value||null;
  if(res[8].status==='fulfilled')data.central=res[8].value||null;
  renderAll();
}
async function pollSlow(){
  if(!live)return;
  const res=await Promise.allSettled([
    getJSON('/api/audit/weights?limit=120'),
    getJSON('/api/audit/events?limit=120'),
    getJSON('/api/audit/weights/verify'),
    getJSON('/api/audit/events/verify')
  ]);
  if(res[0].status==='fulfilled')data.weights=res[0].value.records||[];
  if(res[1].status==='fulfilled')data.events=res[1].value.records||[];
  $('rawVerify').textContent=res[2].status==='fulfilled'&&res[2].value.verified?'VERIFIED':'—';
  $('eventVerify').textContent=res[3].status==='fulfilled'&&res[3].value.verified?'VERIFIED':'—';
  renderChart();renderEvents();
}

function activeCycle(tx){if(!tx?.cycle_id)return null;return (data.queue||[]).find(c=>c.id===tx.cycle_id)||null}
function firstQueued(){return (data.queue||[]).find(c=>c.status==='QUEUED')||null}
function firstCalled(){return (data.queue||[]).find(c=>c.status==='CALLED')||null}

function deriveStage(tx){
  if(data.demo)return data.demo.stage;
  const pass=Number(tx?.pass_number||0),state=tx?.state||data.wf?.state||'IDLE',status=tx?.cycle_status||'';
  if(tx?.business_complete||status==='COMPLETE')return 'COMPLETE';
  if(pass===2){
    if(['WEIGHING','READY_TO_WEIGH','LOCAL_COMMITTED','EXIT_AUTHORIZED','EXITING','COMPLETE'].includes(state))return 'PASS2';
    return 'RETURN';
  }
  if(pass===1){
    if(status==='QUEUED'||['LOCAL_COMMITTED','EXIT_AUTHORIZED','EXITING','COMPLETE'].includes(state))return 'UNLOAD';
    if(['WEIGHING','READY_TO_WEIGH','POSITIONING'].includes(state))return 'PASS1';
    return 'INBOUND';
  }
  if(firstCalled())return 'RETURN';
  if(firstQueued())return 'UNLOAD';
  if(data.latest?.net_kg)return 'COMPLETE';
  return 'INBOUND';
}

function cycleStatus(tx,stage){
  if(data.demo)return data.demo.business;
  if(tx?.business_complete)return 'COMPLETE';
  if(tx?.cycle_status)return tx.cycle_status;
  if(firstCalled())return 'CALLED';
  if(firstQueued())return 'QUEUED';
  if(stage==='COMPLETE'&&data.latest?.net_kg)return 'COMPLETE';
  return 'WAITING';
}

function businessNumbers(tx,cycle){
  const latest=data.latest||{};
  const gross=Number(tx?.gross_kg||cycle?.gross_kg||cycle?.first_pass?.weight?.weight_kg||latest.gross_kg||0);
  const tare=Number(tx?.tare_kg||latest.tare_kg||0);
  const net=Number(tx?.net_kg||latest.net_kg||0);
  return {gross,tare,net};
}

function buildCycleTimeline(){
  $('cycleTimeline').innerHTML=JOURNEY.map((s,i)=>`<div class="cycle-row" data-cycle-step="${s.id}"><i class="cycle-node"></i><div><b>${i+1}. ${s.title}</b><small>${s.copy}</small></div></div>`).join('');
}
buildCycleTimeline();

function setJourney(stage){
  const idx=Math.max(0,JOURNEY.findIndex(s=>s.id===stage));
  document.querySelectorAll('.journey-step').forEach(el=>{const i=JOURNEY.findIndex(s=>s.id===el.dataset.journey);el.classList.toggle('done',i<idx);el.classList.toggle('active',i===idx);el.classList.toggle('upcoming',i>idx)});
  document.querySelectorAll('.cycle-row').forEach(el=>{const i=JOURNEY.findIndex(s=>s.id===el.dataset.cycleStep);el.classList.toggle('done',i<idx);el.classList.toggle('active',i===idx)});
  const step=JOURNEY[idx]||JOURNEY[0];$('sceneNote').innerHTML=`<b>${step.title}</b><span>${step.copy}</span>`;
}

function setCamera(active,captured=[]){
  ['C1A','C3','C1B'].forEach(id=>{
    const el=$('cam'+id);if(el)el.classList.toggle('active',id===active);
    const card=$('ev'+id);if(card){card.classList.toggle('active',id===active);card.classList.toggle('captured',captured.includes(id))}
  });
  $('activeCamera').textContent=active||'—';
  if(active&&active!==lastCamera){lastCamera=active}
}

function renderAll(){
  const h=data.health||{},wf=data.wf||{},sc=data.scale||{},io=data.io||{},identity=data.id||{},st=data.storage||{},tx=wf.transaction||{};
  const stage=deriveStage(tx),business=cycleStatus(tx,stage),cycle=activeCycle(tx)||firstCalled()||firstQueued();
  const {gross,tare,net}=businessNumbers(tx,cycle);
  const direction=tx.direction||data.demo?.direction||'',pass=Number(tx.pass_number||data.demo?.pass||0);
  const plate=identity.lpr?.plate||tx.lpr?.plate||tx.plate||cycle?.plate||data.demo?.plate||data.latest?.plate||'';
  const rfid=identity.rfid?.tag||tx.rfid?.tag||tx.rfid||cycle?.rfid||data.demo?.rfid||data.latest?.rfid||'';

  $('healthDot').className='status-dot '+(h.status==='ok'||data.demo?'ok':'bad');
  $('healthText').textContent=h.status==='ok'||data.demo?'EDGE ONLINE':'EDGE OFFLINE';
  const station=tx.station_id||h.station_id||data.demo?.station||'EDGE-01';$('stationTop').textContent=station;
  $('dbTop').textContent=st.integrity==='ok'||data.demo?'OK':'—';$('syncTop').textContent=st.pending_sync??data.demo?.pending??'—';$('shaMini').textContent=short(h.git_sha||data.demo?.sha||'',10);

  $('cycleTitle').textContent=tx.cycle_id?short(tx.cycle_id,16):(cycle?.id?short(cycle.id,16):(data.latest?.cycle_id?short(data.latest.cycle_id,16):'NO ACTIVE CYCLE'));
  $('vehicleText').textContent=plate||'—';$('rfidText').textContent=rfid||'—';$('directionText').textContent=direction?direction.replace('_TO_',' → '):'—';$('passText').textContent=pass?`PASS ${pass}`:'—';
  $('cycleBadge').textContent=business;$('cycleBadge').className='badge '+(business==='COMPLETE'?'good':business==='CALLED'||business==='QUEUED'?'warn':business.includes('INVALID')||business.includes('MISMATCH')?'bad':'muted');
  $('businessBadge').textContent=business;$('businessBadge').className=$('cycleBadge').className;
  $('physicalBadge').textContent=(tx.state||wf.state||data.demo?.state||'IDLE').replaceAll('_',' ');
  $('cycleClock').textContent=tx.pair_elapsed_seconds?`${tx.pair_elapsed_seconds}s`:data.latest?.pair_elapsed_seconds?`${data.latest.pair_elapsed_seconds}s`:'—';

  setJourney(stage);$('siteScene').dataset.stage=stage;
  $('grossText').textContent=gross?`${fmtN(gross)} kg`:'— kg';$('tareText').textContent=tare?`${fmtN(tare)} kg`:'— kg';$('netText').textContent=net?`${fmtN(net)} kg`:'— kg';$('netState').textContent=net?'valid paired transaction':'waiting for valid pair';

  const reading=sc.last_reading||tx.accepted_weight||data.demo?.reading||{},weight=Number(reading.weight_kg??0),stable=!!reading.stable||!!tx.accepted_weight||!!data.demo?.stable;
  $('weight').textContent=`${fmtN(weight)} kg`;$('weight').className=stable?'stable':'';$('weightState').textContent=sc.connected||data.demo?(stable?'STABLE · RAW-AUDITED':'LIVE · UNSTABLE'):(tx.accepted_weight?'ACCEPTED PASS WEIGHT':'WAITING FOR LIVE FRAME');
  $('scaleDeck').classList.toggle('active',['PASS1','PASS2'].includes(stage));

  const inp=io.last_input||data.demo?.io?.last_input||{},ap=io.applied||data.demo?.io?.applied||{};
  $('sEntry').classList.toggle('on',!!inp.entry_present);$('sFront').classList.toggle('on',!!inp.front_present);$('sRear').classList.toggle('on',!!inp.rear_present);$('sExit').classList.toggle('on',!!inp.exit_present);
  $('barrierA').classList.toggle('open',!!inp.entry_barrier_open||!!ap.entry_barrier_open);$('barrierA').classList.toggle('go',!!ap.entry_green);
  $('barrierB').classList.toggle('open',!!inp.exit_barrier_open||!!ap.exit_barrier_open);$('barrierB').classList.toggle('go',!!ap.exit_green);

  const truck=$('truck');truck.classList.toggle('empty',['RETURN','PASS2','COMPLETE'].includes(stage));truck.classList.toggle('loaded',['INBOUND','PASS1','UNLOAD'].includes(stage));truck.classList.toggle('dumping',stage==='UNLOAD');truck.classList.toggle('moving',['INBOUND','RETURN','COMPLETE'].includes(stage));truck.classList.toggle('unloading',stage==='UNLOAD');
  $('plateTruck').textContent=plate||'—';

  let cam='';const captured=[];
  if(['INBOUND'].includes(stage)){cam='C1A';}
  if(stage==='PASS1'){cam='C3';captured.push('C1A')}
  if(stage==='UNLOAD'){cam='C3';captured.push('C1A','C3')}
  if(stage==='RETURN'){cam='C1B';captured.push('C1A','C3')}
  if(stage==='PASS2'){cam='C3';captured.push('C1A','C3','C1B')}
  if(stage==='COMPLETE'){captured.push('C1A','C3','C1B')}
  setCamera(cam,captured);
  $('evC1AText').textContent=captured.includes('C1A')?'CAPTURED':cam==='C1A'?'ACTIVE':'WAITING';$('evC3Text').textContent=captured.includes('C3')?'CAPTURED':cam==='C3'?'ACTIVE':'WAITING';$('evC1BText').textContent=captured.includes('C1B')?'CAPTURED':cam==='C1B'?'ACTIVE':'WAITING';

  $('sigScale').className=sc.connected||data.demo?'ok':sc.enabled===false?'':'bad';$('devScale').textContent=sc.connected||data.demo?`CONNECTED · ${stable?'STABLE':'STREAMING'}`:(sc.enabled===false?'DISABLED':'DISCONNECTED');
  $('sigIO').className=io.connected||data.demo?'ok':io.enabled===false?'':'bad';$('devIO').textContent=io.connected||data.demo?'MODBUS CONNECTED':(io.enabled===false?'DISABLED':'DISCONNECTED');
  $('sigRFID').className=rfid?'ok':'';$('devRFID').textContent=rfid||'WAITING';$('sigLPR').className=plate?'ok':'';$('devLPR').textContent=plate?`C1 CAPTURE · ${plate}`:'C1A · C1B · C3';

  $('queued').textContent=st.queued_cycles??data.demo?.queued??0;$('called').textContent=st.called_cycles??data.demo?.called??0;$('completed').textContent=st.completed_cycles??data.demo?.completed??0;$('pending').textContent=st.pending_sync??data.demo?.pending??0;
  renderQueue();

  document.documentElement.dataset.uiState=tx.state||wf.state||data.demo?.state||'IDLE';
  document.documentElement.dataset.uiWeight=String(weight);
  document.documentElement.dataset.uiPlate=plate;
  document.documentElement.dataset.uiMode=live?'live':'demo';
  document.documentElement.dataset.uiCycle=business;
  document.documentElement.dataset.uiStage=stage;
}

function renderQueue(){
  const q=data.demo?.queue||data.queue||[];
  if(!q.length){$('queueList').innerHTML='<div class="empty">No queued first-pass vehicles</div>';return}
  $('queueList').innerHTML=q.map(c=>{const gross=c.first_pass?.weight?.weight_kg||c.gross_kg||0,age=c.queued_at?Math.max(0,Math.round((Date.now()-new Date(c.queued_at).getTime())/60000)):0,call=c.status==='QUEUED'&&live?`<button class="btn call" data-call="${esc(c.id)}">CALL</button>`:'';return `<div class="queue-card ${c.status==='CALLED'?'called':''}"><div class="row"><div><b>${esc(c.plate||'UNKNOWN')} · ${esc(c.status)}</b><small>${fmtN(gross)} kg gross · ${age} min · ${esc(short(c.id,10))}</small></div>${call}</div></div>`}).join('');
  document.querySelectorAll('[data-call]').forEach(b=>b.onclick=()=>callCycle(b.dataset.call));
}
async function callCycle(id){if(!live||!id)return;try{await postJSON(`/api/queue/${encodeURIComponent(id)}/call`);showToast('CALLED — only this matching B→A truck may enter',false);await pollFast()}catch(e){showToast(e.message,true)}}
function showToast(msg,bad=false){const t=$('toast');t.textContent=msg;t.className='toast '+(bad?'bad ':'')+'show';setTimeout(()=>t.classList.remove('show'),3000)}

function renderChart(){
  const rec=data.weights||[];if(!rec.length){$('chartLine').setAttribute('d','');$('chartStable').setAttribute('d','');$('chartDot').setAttribute('cx','0');$('chartDot').setAttribute('cy','108');return}
  const vals=rec.map(r=>({w:Number(r.event?.weight_kg??0),stable:!!r.event?.stable})),max=Math.max(1000,...vals.map(v=>Math.abs(v.w)))*1.08,min=Math.min(0,...vals.map(v=>v.w)),range=max-min||1;
  const pts=vals.map((v,i)=>({x:i/(Math.max(1,vals.length-1))*1000,y:106-((v.w-min)/range)*96}));
  $('chartLine').setAttribute('d',pts.map((p,i)=>(i?'L':'M')+p.x.toFixed(1)+' '+p.y.toFixed(1)).join(' '));
  let seg='',open=false;pts.forEach((p,i)=>{if(vals[i].stable){seg+=(open?' L':' M')+p.x.toFixed(1)+' '+p.y.toFixed(1);open=true}else open=false});$('chartStable').setAttribute('d',seg);
  const p=pts[pts.length-1];$('chartDot').setAttribute('cx',p.x);$('chartDot').setAttribute('cy',p.y);$('chartMeta').textContent=`${vals.length} audited frames · peak ${fmtN(Math.max(...vals.map(v=>v.w)))} kg`;
}
function renderEvents(){
  const rows=(data.events||[]).slice(-40).reverse();if(!rows.length){$('timeline').innerHTML='<div class="empty">Waiting for operational audit events…</div>';return}
  $('timeline').innerHTML=rows.map(r=>{const e=r.event||{},k=e.kind||'EVENT',cl=k.includes('OUTPUT')?'cmd':k.includes('TICKET')||k.includes('COMPLETE')?'ticket':k.includes('FAULT')?'fault':'',detail=e.action||e.reason||e.new_state||e.device||'',tm=e.at_utc?new Date(e.at_utc).toLocaleTimeString([],{hour12:false}):'';return `<div class="event-card ${cl}"><b>#${r.seq||'?'} ${esc(k)}</b><small>${esc(tm)} ${esc(detail)}</small></div>`}).join('');
}

const DEMO_STEPS=[
  {stage:'INBOUND',state:'APPROACH',business:'WAITING',direction:'A_TO_B',pass:1,weight:0,stable:true,loaded:true,queued:0,called:0,completed:0,pending:0,duration:1700},
  {stage:'PASS1',state:'WEIGHING',business:'WAITING',direction:'A_TO_B',pass:1,weight:28420,stable:false,loaded:true,queued:0,called:0,completed:0,pending:0,duration:1050},
  {stage:'PASS1',state:'LOCAL_COMMITTED',business:'QUEUED',direction:'A_TO_B',pass:1,weight:28460,stable:true,gross:28460,queued:1,called:0,completed:0,pending:0,duration:1400},
  {stage:'UNLOAD',state:'IDLE',business:'QUEUED',direction:'A_TO_B',pass:1,weight:0,stable:true,gross:28460,queued:1,called:0,completed:0,pending:0,duration:2600},
  {stage:'RETURN',state:'APPROACH',business:'CALLED',direction:'B_TO_A',pass:2,weight:0,stable:true,gross:28460,queued:0,called:1,completed:0,pending:0,duration:1800},
  {stage:'PASS2',state:'WEIGHING',business:'CALLED',direction:'B_TO_A',pass:2,weight:11840,stable:false,gross:28460,queued:0,called:1,completed:0,pending:0,duration:950},
  {stage:'PASS2',state:'LOCAL_COMMITTED',business:'COMPLETE',direction:'B_TO_A',pass:2,weight:11820,stable:true,gross:28460,tare:11820,net:16640,queued:0,called:0,completed:1,pending:1,duration:1500},
  {stage:'COMPLETE',state:'COMPLETE',business:'COMPLETE',direction:'B_TO_A',pass:2,weight:0,stable:true,gross:28460,tare:11820,net:16640,queued:0,called:0,completed:1,pending:1,duration:2600}
];

function applyDemoStep(step){
  const plate='15C-123.45',rfid='RFID-DEMO-001',gross=step.gross||0,tare=step.tare||0,net=step.net||0;
  data.demo={...step,plate,rfid,station:'DEMO-EDGE',sha:'visual-demo',reading:{weight_kg:step.weight,stable:step.stable},io:fakeIO(step),queue:step.business==='QUEUED'?[{id:'DEMO-CYCLE-001',plate,rfid,status:'QUEUED',gross_kg:gross,first_pass:{weight:{weight_kg:gross}},queued_at:new Date(Date.now()-12*60000).toISOString()}]:step.business==='CALLED'?[{id:'DEMO-CYCLE-001',plate,rfid,status:'CALLED',gross_kg:gross,first_pass:{weight:{weight_kg:gross}},queued_at:new Date(Date.now()-15*60000).toISOString()}]:[]};
  data.health={status:'ok',git_sha:'visual-demo',two_pass_cycle:true};
  data.wf={state:step.state,mode:'NORMAL',transaction:{id:`DEMO-PASS-${step.pass}`,station_id:'DEMO-EDGE',state:step.state,direction:step.direction,pass_number:step.pass,cycle_id:'DEMO-CYCLE-001',cycle_status:step.business==='WAITING'?'':step.business,business_complete:step.business==='COMPLETE',gross_kg:gross,tare_kg:tare,net_kg:net,pair_elapsed_seconds:2712,lpr:{plate},rfid:{tag:rfid},position:'ACCEPTED'}};
  data.scale={enabled:true,connected:true,last_reading:{weight_kg:step.weight,stable:step.stable}};data.io=data.demo.io;data.id={rfid:{tag:rfid},lpr:{plate}};data.storage={integrity:'ok',queued_cycles:step.queued,called_cycles:step.called,completed_cycles:step.completed,pending_sync:step.pending};data.queue=data.demo.queue;data.latest=net?{cycle_id:'DEMO-CYCLE-001',plate,rfid,gross_kg:gross,tare_kg:tare,net_kg:net,pair_elapsed_seconds:2712}:null;
  data.weights.push({seq:data.weights.length+1,event:{weight_kg:step.weight,stable:step.stable},hash:'demo'});if(data.weights.length>120)data.weights.shift();
  data.events.push({seq:data.events.length+1,event:{kind:step.business==='COMPLETE'?'TICKET_COMMITTED':'STATE_TRANSITION',action:`${step.stage} · ${step.business}`,at_utc:new Date().toISOString()},hash:'demo'});if(data.events.length>120)data.events.shift();
  $('rawVerify').textContent='VERIFIED';$('eventVerify').textContent='VERIFIED';renderAll();renderChart();renderEvents();
}
function fakeIO(step){
  const a=step.direction==='A_TO_B',b=step.direction==='B_TO_A',approach=step.state==='APPROACH',weigh=['WEIGHING','LOCAL_COMMITTED'].includes(step.state);
  return {enabled:true,connected:true,last_input:{entry_present:a&&approach,exit_present:b&&approach,front_present:weigh,rear_present:weigh,entry_barrier_open:a&&approach,exit_barrier_open:b&&approach,safety_clear:true},applied:{entry_barrier_open:a&&approach,entry_green:a&&approach,exit_barrier_open:b&&approach,exit_green:b&&approach,buzzer:false}};
}
function startDemo(){
  live=false;clearTimeout(demoTimer);data.weights=[];data.events=[];demoIndex=0;$('demoBanner').classList.add('show');$('demoBtn').classList.add('primary');$('liveBtn').classList.remove('primary');
  const run=()=>{const step=DEMO_STEPS[demoIndex];applyDemoStep(step);demoIndex=(demoIndex+1)%DEMO_STEPS.length;demoTimer=setTimeout(run,step.duration)};run();
}
function holdDemoWeighing(){
  live=false;clearTimeout(demoTimer);data.weights=[];data.events=[];$('demoBanner').classList.add('show');$('demoBtn').classList.add('primary');$('liveBtn').classList.remove('primary');applyDemoStep({stage:'PASS1',state:'WEIGHING',business:'WAITING',direction:'A_TO_B',pass:1,weight:28420,stable:false,gross:0,queued:0,called:0,completed:0,pending:0,duration:0});
}
function startLive(){live=true;data.demo=null;clearTimeout(demoTimer);demoTimer=null;$('demoBanner').classList.remove('show');$('liveBtn').classList.add('primary');$('demoBtn').classList.remove('primary');pollFast();pollSlow()}

$('demoBtn').addEventListener('click',startDemo);$('liveBtn').addEventListener('click',startLive);
setInterval(()=>{$('clock').textContent=new Date().toLocaleTimeString([],{hour12:false})},1000);setInterval(pollFast,350);setInterval(pollSlow,1300);
const query=new URLSearchParams(location.search),demo=query.get('demo');
if(demo==='1')startDemo();else if(demo==='WEIGHING')holdDemoWeighing();else{live=true;$('liveBtn').classList.add('primary');pollFast();pollSlow()}
window.addEventListener('unhandledrejection',e=>showToast('UI warning: '+(e.reason?.message||e.reason||'request failed'),true));
