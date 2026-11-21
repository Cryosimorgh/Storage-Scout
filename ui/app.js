(() => {
'use strict';
const key=new URLSearchParams(location.search).get('key')||'';
const state={view:'overview',root:'',drives:[],progress:null,summary:null,files:[],folders:[],duplicates:[],search:'',polling:false,everLoaded:false,renderedSignature:'',openPaths:[]};
const $=id=>document.getElementById(id);
const C={Videos:'#796ce9',Archives:'#e8ac59',Images:'#e4849b',Applications:'#36b4a6',Documents:'#5689d1',Audio:'#64c7d7','Data & backups':'#977de2',Code:'#7688a1',Other:'#c2cad8'};
const e=s=>String(s??'').replace(/[&<>"']/g,ch=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));
const fmt=(v)=>{let n=Number(v||0);if(n<1024)return n+' B';const units=['KB','MB','GB','TB','PB'];let i=-1;do{n/=1024;i++}while(n>=1024&&i<units.length-1);return n.toFixed(n>=100?0:n>=10?1:2)+' '+units[i]};
const count=n=>Number(n||0).toLocaleString();
const shortDate=s=>{if(!s)return '—';let d=new Date(s);return Number.isNaN(d.getTime())?'—':d.toLocaleDateString(undefined,{month:'short',day:'numeric',year:'numeric'})};
const basename=s=>String(s||'').replace(/[\\/]+$/,'').split(/[\\/]/).pop()||s;
const dirname=s=>String(s||'').slice(0,Math.max(0,String(s||'').length-basename(s).length));
const percent=(x,total)=>total?Math.max(0,Math.min(100,100*x/total)):0;
const percentLabel=(x,total)=>percent(x,total).toFixed(1)+'%';
const badge=(cat)=>cat==='Archives'?'archive':cat==='Videos'?'video':cat==='Images'?'image':cat==='Applications'?'app':'normal';
const glyph=(cat)=>cat==='Archives'?'ZIP':cat==='Videos'?'▶':cat==='Images'?'▧':cat==='Applications'?'APP':cat==='Audio'?'♫':cat==='Documents'?'DOC':cat==='Data & backups'?'DB':'FILE';
const get=async(path)=>{const res=await fetch('/api/'+path,{headers:{'X-Scout-Key':key}});if(!res.ok)throw new Error((await res.json()).error||res.statusText);return res.json()};
const post=async(path,data={})=>{const res=await fetch('/api/'+path,{method:'POST',headers:{'X-Scout-Key':key,'Content-Type':'application/json'},body:JSON.stringify(data)});if(!res.ok)throw new Error((await res.json()).error||res.statusText);return res.json()};
let toastTimer=0;function toast(s){const el=$('toast');el.textContent=s;el.classList.remove('hidden');clearTimeout(toastTimer);toastTimer=setTimeout(()=>el.classList.add('hidden'),4500)}
function selectedDrive(){return state.drives.find(d=>state.root.toLowerCase().startsWith(d.path.toLowerCase()))||state.drives[0]||null}
function setHeader(){
const title={overview:'Your storage, explained.',large:'Largest files',folders:'Folder breakdown',old:'Old file candidates',duplicates:'Duplicate files',types:'File type analysis'};
const sub={overview:"A clear picture of what's using your disk. No guesswork, no risky cleanup.",large:'Find the biggest files first, ranked by logical file size.',folders:'See which directories account for the most storage.',old:'Files unchanged for over a year. Age does not mean unused.',duplicates:'Verify exact byte-for-byte duplicate content using SHA-256.',types:'Understand where your storage is concentrated by category.'};
$('pageTitle').textContent=title[state.view];$('pageSubtitle').textContent=sub[state.view];$('crumbCurrent').textContent=state.view==='large'?'Large files':state.view.charAt(0).toUpperCase()+state.view.slice(1);
$('scanState').textContent=state.progress?.running?(state.progress.phase==='verifying'?'VERIFYING CONTENT':'SCAN IN PROGRESS'):(state.progress?.phase==='complete'?'SCAN COMPLETE':'READY TO SCAN');
$('scanButton').disabled=!!state.progress?.running;$('scanButton').innerHTML=state.progress?.running?'<span class="spin"></span> Working…':'<span>⌕</span> Start scan';
$('exportButton').disabled=!state.summary;
document.querySelectorAll('.nav-link').forEach(b=>b.classList.toggle('active',b.dataset.view===state.view));
}
function drives(){const cont=$('driveButtons');cont.innerHTML=state.drives.slice(0,6).map(d=>`<button class="drive-chip ${state.root.toLowerCase().startsWith(d.path.toLowerCase())?'active':''}" data-drive="${e(d.path)}" title="${e(d.path)}">${e(d.path.replace(/[\\/]/g,''))}</button>`).join('');}
function progress(){const p=state.progress;const el=$('progressSlot');if(!p){el.innerHTML='';return}
 if(p.running){el.innerHTML=`<div class="progress"><div class="spin"></div><span>${p.phase==='verifying'?'Verifying duplicate candidates':'Scanning files'} · ${e(basename(p.path)||p.path)}</span><span class="count">${p.phase==='verifying'?`${count(p.verifiedFiles)} / ${count(p.verifyTotal)}`:`${count(p.files)} files · ${fmt(p.bytes)}`}</span><button class="tiny-link" id="cancelTask">Cancel</button></div>`;
 }else if(p.phase==='cancelled'){el.innerHTML=`<div class="progress"><span>⊘ Scan cancelled. No files were modified.</span></div>`}
 else if(p.errors){el.innerHTML=`<div class="progress"><span>ⓘ Scan complete with ${count(p.errors)} inaccessible or changed entries.</span></div>`}
 else{el.innerHTML=''}
}
function statCards(){const r=state.summary;return `<div class="stats">
  <div class="stat"><span class="label">SPACE INDEXED</span><span class="stat-icon">▱</span><div class="value">${fmt(r.totalBytes)}</div><div class="hint">Logical size of scanned files</div></div>
  <div class="stat"><span class="label">FILES DISCOVERED</span><span class="stat-icon">▤</span><div class="value">${count(r.fileCount)}</div><div class="hint">Across ${count(r.folderCount)} folders</div></div>
  <div class="stat"><span class="label">FILES OLDER THAN 1 YEAR</span><span class="stat-icon">◷</span><div class="value">${fmt(r.oldBytes)}</div><div class="hint">${count(r.oldCount)} age-based candidates</div></div>
  <div class="stat"><span class="label">VERIFIED DUPLICATE DATA</span><span class="stat-icon">◈</span><div class="value">${fmt(r.duplicateBytes)}</div><div class="hint">${r.duplicates?.length?`${count(r.duplicates.length)} verified groups`:'Run duplicate verification'}</div></div>
</div>`}
function typePanel(){const r=state.summary;const types=(r.types||[]).slice(0,7);return `<div class="panel panel-pad"><div class="panel-head"><div><h2>What's taking space</h2><div class="panel-sub">Space by file category</div></div><button class="tiny-link" data-go="types">Explore all →</button></div><div class="bar-stack">${types.map(t=>`<span style="flex:${t.bytes||0};background:${C[t.category]||C.Other}"></span>`).join('')}</div><div class="legend">${types.map(t=>`<div class="legend-item"><i class="color-box" style="background:${C[t.category]||C.Other}"></i><span>${e(t.category)}</span><strong>${percentLabel(t.bytes,r.totalBytes)}</strong></div>`).join('')}</div>${driveOverview()}</div>`}
function driveOverview(){const d=selectedDrive();if(!d||!d.total)return '';const used=d.total-d.free;return `<div class="mini-drive"><span class="disc">▣</span><div><strong>${e(d.path)} · Drive capacity</strong><small>${fmt(used)} used of ${fmt(d.total)} total</small></div><div class="right"><strong>${fmt(d.free)} free</strong><small>${percentLabel(d.free,d.total)} available</small></div></div>`}
function folderPanel(){const list=(state.folders||[]).slice(0,5);return `<div class="panel panel-pad"><div class="panel-head"><div><h2>Heaviest folders</h2><div class="panel-sub">Largest directory totals within this scan</div></div><button class="tiny-link" data-go="folders">View folders →</button></div><div class="folder-list">${list.length?list.map(f=>`<div class="folder-line"><div class="folder-glyph">▣</div><div class="folder-meta"><div class="folder-name">${e(f.name)}</div><div class="folder-path">${e(f.path)}</div></div><span class="folder-size">${fmt(f.size)}</span></div>`).join(''):'<div class="placeholder">No folders found.</div>'}</div></div>`}
function table(rows,{type='files',limit=12}={}){const take=rows.slice(0,limit);state.openPaths=take.map(x=>x.path);
return `<div class="table-scroll"><table class="file-table"><thead><tr><th>${type==='folders'?'FOLDER':'FILE NAME / LOCATION'}</th><th>SIZE</th><th>${type==='folders'?'RELATIVE SPACE':'LAST MODIFIED'}</th><th>INSPECT</th></tr></thead><tbody>${take.length?take.map((f,i)=>{
const folder=type==='folders';const p=folder?f.path:f.path;const b=folder?'folder':badge(f.category);
return `<tr><td><div class="file-row"><span class="file-badge ${b}">${folder?'▣':glyph(f.category)}</span><div class="folder-meta"><div class="file-name" title="${e(f.name)}">${e(f.name)}</div><div class="file-dir" title="${e(p)}">${e(dirname(p))}</div></div></div></td><td><div class="size-main">${fmt(f.size)}</div><div class="size-track"><i style="width:${percent(f.size,(state.summary?.totalBytes||1)).toFixed(2)}%"></i></div></td><td>${folder?`<span class="tag">${percentLabel(f.size,state.summary?.totalBytes)}</span>`:`<span>${shortDate(f.modified)}</span>`}</td><td class="row-actions"><button data-open="${i}">Show in folder ↗</button></td></tr>`}).join(''):`<tr><td colspan="4"><div class="placeholder">No matching ${folderTitle(type)} found.</div></td></tr>`}</tbody></table></div><div class="table-bottom"><span>${count(take.length)} displayed · ${count(rows.length)} loaded</span><span>Read-only results · No files are modified</span></div>`}
function folderTitle(type){return type==='folders'?'folders':'files'}
function tableCard(title,desc,rows,type='files',limit=10,searchBox=false){return `<div class="panel table-panel"><div class="table-head"><div><h2>${e(title)}</h2><div class="panel-sub">${e(desc)}</div></div>${searchBox?`<div class="table-actions"><label class="search">⌕ <input id="searchInput" placeholder="Filter by path..." value="${e(state.search)}" /></label></div>`:`<button class="tiny-link" data-go="large">View all files →</button>`}</div>${table(rows,{type,limit})}</div>`}
function overview(){const r=state.summary;return statCards()+`<div class="section-grid">${typePanel()}${folderPanel()}</div>`+tableCard('Largest files','Top files by logical size',state.files,'files',8)}
function detailedFiles(view){const old=view==='old';return `${statCards()}${old?'<div class="notice"><strong>Age is not a usage signal.</strong> Windows may not track reliable last-access times. These files have not been modified for at least 365 days, but may still be important.</div>':''}`+tableCard(old?'Old file candidates':'Largest files',old?'Sorted by size among files last modified over a year ago':'Sorted by logical size; directories and sparse files can affect physical disk usage',state.files,'files',1500,true)}
function folderView(){return statCards()+tableCard('Folders by size','Includes each folder’s descendant files; nested totals overlap',state.folders,'folders',800,true)}
function duplicateView(){const r=state.summary;const groups=state.duplicates||[];
return `<div class="insight-grid"><div class="insight"><div class="label">RECOVERABLE DUPLICATE BYTES</div><div class="value">${fmt(r.duplicateBytes)}</div><div class="info">Sum of extra copies, not automatically safe to delete</div></div><div class="insight"><div class="label">MATCHING CONTENT GROUPS</div><div class="value">${count(groups.length)}</div><div class="info">Exact SHA-256 matches</div></div><div class="insight"><div class="label">FILES INDEXED</div><div class="value">${count(r.fileCount)}</div><div class="info">Candidates grouped by file size first</div></div></div><div class="notice"><strong>Verification reads file contents.</strong> This can take time on large drives. Matching content does not imply an extra copy is unnecessary; linked files, backups and application assets may be intentional.</div><div class="panel full-panel"><div class="table-head"><div><h2>Verified duplicates</h2><div class="panel-sub">Exact content matches, sorted by potential space savings</div></div><button class="btn primary" id="verifyButton" ${state.progress?.running?'disabled':''}>◈ ${groups.length?'Verify again':'Verify duplicate files'}</button></div>${groups.length?groups.slice(0,200).map(g=>`<div class="dup-item"><div class="dup-paths"><strong>${count(g.paths.length)} copies · ${fmt(g.size)} each</strong><div>${g.paths.map(p=>e(p)).join(' • ')}</div></div><div class="right">${fmt(g.size*(g.paths.length-1))} extra</div></div>`).join(''):'<div class="placeholder">No verified duplicate groups yet. Run verification after a scan to detect files with identical content.</div>'}</div>`}
function typeView(){const r=state.summary;return statCards()+`<div class="panel full-panel"><div class="table-head"><div><h2>Storage by category</h2><div class="panel-sub">Classification by file extension. Unrecognized extensions appear as Other.</div></div></div>${(r.types||[]).map(t=>`<div class="category-row"><span class="color-box" style="background:${C[t.category]||C.Other}"></span><span class="category-name">${e(t.category)}</span><span class="category-progress"><i style="width:${percent(t.bytes,r.totalBytes)}%;background:${C[t.category]||C.Other}"></i></span><span class="category-bytes">${fmt(t.bytes)}</span><span class="category-count">${count(t.count)} files</span></div>`).join('')}</div>`}
function render(){setHeader();drives();progress();const el=$('content');const r=state.summary;
 if(!r){el.innerHTML=`<div class="initial-state"><div class="initial-icon">⌕</div><h2>${state.progress?.running?'Scanning your selected location':'Discover your storage footprint'}</h2><p>${state.progress?.running?'Collecting file metadata. Your files will remain untouched.':'Choose a drive above and start a scan to reveal large files, old candidates, duplicates, and folder sizes.'}</p><span>Everything stays on your device.</span></div>`;return}
 switch(state.view){case 'overview':el.innerHTML=overview();break;case 'large':case 'old':el.innerHTML=detailedFiles(state.view);break;case 'folders':el.innerHTML=folderView();break;case 'duplicates':el.innerHTML=duplicateView();break;case 'types':el.innerHTML=typeView();break;}
}
async function refreshRows(){if(!state.summary)return;
 const view=state.view==='old'?'old':'large';
 if(state.view==='overview'||state.view==='large'||state.view==='old'){const data=await get(`files?view=${view}&query=${encodeURIComponent(state.search)}&limit=1500`);state.files=data.files||[]}
 if(state.view==='overview'||state.view==='folders'){const data=await get(`folders?query=${encodeURIComponent(state.search)}&limit=800`);state.folders=data.folders||[]}
 if(state.view==='duplicates'){const data=await get('duplicates');state.duplicates=data.duplicates||[]}
}
let completedKey='';async function tick(){if(state.polling)return;state.polling=true;
 try{const data=await get('status');state.progress=data.progress;state.summary=data.summary||null;
 let newKey=(data.summary?.finished||'')+':'+(data.summary?.duplicateBytes||0)+':'+(data.progress?.phase||'')+':'+(data.progress?.verifiedFiles||0);
 if(state.summary&&newKey!==completedKey&&!(state.progress?.running)){completedKey=newKey;await refreshRows()}
 render();
 }catch(err){toast('Connection error: '+err.message)}finally{state.polling=false}
}
async function changeView(view){state.view=view;state.search='';await refreshRows();render();}
async function startScan(){try{state.root=$('pathInput').value.trim();await post('scan',{path:state.root});state.search='';state.files=[];state.folders=[];state.duplicates=[];completedKey='';await tick()}catch(err){toast(err.message)}}
async function exportCSV(){if(!state.summary){toast('Scan a folder first');return}
 const view=['old','folders','duplicates'].includes(state.view)?state.view:'large';
 try{const res=await fetch('/api/export?view='+view,{headers:{'X-Scout-Key':key}});if(!res.ok)throw new Error('Export failed');const blob=await res.blob();const u=URL.createObjectURL(blob);const a=document.createElement('a');a.href=u;a.download=`storage-scout-${view}.csv`;document.body.appendChild(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(u),1000)}catch(err){toast(err.message)}
}
$('navigation').addEventListener('click',e=>{const btn=e.target.closest('[data-view]');if(btn)changeView(btn.dataset.view).catch(err=>toast(err.message))});
$('driveButtons').addEventListener('click',e=>{const b=e.target.closest('[data-drive]');if(b){$('pathInput').value=b.dataset.drive;state.root=b.dataset.drive;drives()}});
$('scanButton').addEventListener('click',startScan);
$('exportButton').addEventListener('click',exportCSV);
$('pathInput').addEventListener('keydown',e=>{if(e.key==='Enter')startScan()});
$('quitButton').addEventListener('click',()=>post('quit').catch(err=>toast(err.message)));
let debounce=0;$('content').addEventListener('input',evt=>{if(evt.target.id!=='searchInput')return;state.search=evt.target.value;clearTimeout(debounce);debounce=setTimeout(async()=>{try{await refreshRows();const search=$('searchInput');const pos=search?.selectionStart;render();const restored=$('searchInput');if(restored){restored.focus();try{restored.setSelectionRange(pos,pos)}catch{}}}catch(err){toast(err.message)}},260)});
$('content').addEventListener('click',evt=>{
 const go=evt.target.closest('[data-go]');if(go){changeView(go.dataset.go).catch(err=>toast(err.message));return}
 const open=evt.target.closest('[data-open]');if(open){const p=state.openPaths[Number(open.dataset.open)];if(p)post('reveal',{path:p}).catch(err=>toast(err.message));return}
 if(evt.target.closest('#verifyButton')){post('verify').then(()=>tick()).catch(err=>toast(err.message))}
});
$('progressSlot').addEventListener('click',evt=>{if(evt.target.id==='cancelTask')post('cancel').then(()=>tick()).catch(err=>toast(err.message))});
(async()=>{try{const data=await get('drives');state.root=data.defaultRoot||'';state.drives=data.drives||[];$('pathInput').value=state.root;await tick();}catch(err){toast('Unable to connect to the local scanner: '+err.message)}setInterval(tick,950)})();
})();