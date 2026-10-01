import {validateConfig,safeHref,normalizeHref,readEnvelope} from './contract.mjs';
const $=s=>document.querySelector(s), categories={all:'全部',text:'文本',code:'代码',image:'图像',video:'视频'};
const externalHref=/^(?:https:\/\/|http:\/\/(?:qm|qun|jq|shang)\.qq\.com(?:\/|$))/i;
let config,filter='all',query='',timer;
const storage={get:k=>{try{return localStorage.getItem(k);}catch{return null;}},set:(k,v)=>{try{localStorage.setItem(k,v);}catch{}}};
const day=()=>new Date().toLocaleDateString('en-CA');
function toast(t){$('#toast').textContent=t;$('#toast').classList.add('visible');clearTimeout(timer);timer=setTimeout(()=>$('#toast').classList.remove('visible'),2300);}
function link(a){const e=document.createElement('a');e.textContent=a.label;e.href=safeHref(a.href)?normalizeHref(a.href):'/login';e.className=a.variant==='text'?'':'btn '+(a.variant==='primary'?'primary':'');if(externalHref.test(e.href)){e.target='_blank';e.rel='noopener noreferrer';}return e;}
const activeActions=a=>a.filter(x=>x.enabled);
async function getJSON(url){const r=await fetch(url,{credentials:'omit',cache:'no-cache',signal:AbortSignal.timeout(6000)});if(!r.ok)throw new Error('配置响应错误');const raw=await r.text();if(raw.length>260000)throw new Error('配置超出限制');return JSON.parse(raw);}
function activeNotice(n){const now=Date.now();return n.enabled&&(!n.starts_at||Date.parse(n.starts_at)<=now)&&(!n.ends_at||now<Date.parse(n.ends_at));}
function noticeKey(){return `relay-home:notice:${config.announcement.id}:${config.announcement.version}`;}
function renderModels(){const host=$('#model-list');host.replaceChildren();const items=config.models.items.filter(m=>m.enabled&&(filter==='all'||m.category===filter)&&(m.label.toLowerCase().includes(query)||m.id.toLowerCase().includes(query)));
  for(const m of items){const chip=document.createElement('span');chip.className='model-chip';const name=document.createElement('span');name.textContent=m.label;const cat=document.createElement('small');cat.textContent=categories[m.category];chip.append(name,cat);host.append(chip);}
  if(!items.length){const p=document.createElement('p');p.className='muted';p.textContent='暂无匹配的模型。';host.append(p);}
  $('#model-meta').textContent=`展示 ${items.length} 个模型 · 实际权限以控制台为准${config.models.observed_at?' · 目录更新于 '+new Date(config.models.observed_at).toLocaleString('zh-CN'):''}`;
}
function render(c){config=c;document.title=c.brand.name+' · AI API 服务';$('#brand').textContent=c.brand.name;$('#footer-brand').textContent=c.brand.name;$('#logo').textContent=c.brand.logo_text;if(c.brand.logo_url){const img=document.createElement('img');img.src=c.brand.logo_url;img.alt='';$('#logo').replaceChildren(img);}
  for(const [id,v] of Object.entries({'#badge':c.hero.badge,'#hero-title':c.hero.title,'#hero-accent':c.hero.accent,'#hero-description':c.hero.description,'#api-label':c.api.label,'#base-url':c.api.base_url,'#api-hint':c.api.hint,'#models-title':c.models.title,'#models-description':c.models.description,'#copyright':c.footer.copyright}))$(id).textContent=v;
  $('#navigation').replaceChildren(...activeActions(c.navigation).map(link));$('#actions').replaceChildren(...activeActions(c.actions).map(link));$('#footer-links').replaceChildren(...activeActions(c.footer.links).map(link));
  $('#api-section').hidden=!c.api.enabled;$('#copy').hidden=!c.api.copy_enabled;$('#copy').disabled=false;$('#models').hidden=!c.models.enabled;
  $('#filters').replaceChildren(...Object.entries(categories).filter(([id])=>id==='all'||c.models.items.some(m=>m.enabled&&m.category===id)).map(([id,label])=>{const b=document.createElement('button');b.className='filter';b.textContent=label;b.setAttribute('aria-pressed',String(id===filter));b.onclick=()=>{filter=id;for(const x of $('#filters').children)x.setAttribute('aria-pressed',String(x===b));renderModels();};return b;}));renderModels();
  const icons={layers:'◈',key:'⌘',shield:'◇',bolt:'↯',globe:'◎',chart:'▤'};
  $('#features').replaceChildren(...c.capabilities.filter(x=>x.enabled).map(f=>{const art=document.createElement('article');art.className='feature';const icon=document.createElement('span');icon.className='feature-icon';icon.textContent=icons[f.icon];icon.setAttribute('aria-hidden','true');const h=document.createElement('h3');h.textContent=f.title;const p=document.createElement('p');p.textContent=f.description;art.append(icon,h,p);return art;}));
  const n=c.announcement,show=activeNotice(n);$('#notice-section').hidden=!show;
  if(show){$('#inline-title').textContent=n.title;$('#inline-body').textContent=n.mode==='banner'?n.body:n.body.split('\n')[0];$('#notice-title').textContent=n.title;$('#notice-date').textContent=n.date;$('#notice-body').textContent=n.body;$('#notice-actions').replaceChildren(...activeActions(n.actions).map(link));const dismissed=storage.get(noticeKey());if(n.mode==='popup'&&dismissed!=='never'&&!(n.policy==='daily'&&dismissed===day())&&!(n.policy==='once'&&dismissed==='once'))$('#notice-dialog').showModal();}
}
$('#menu-toggle').onclick=()=>{const open=$('#navigation').classList.toggle('open');$('#menu-toggle').setAttribute('aria-expanded',String(open));};
$('#model-search').oninput=e=>{query=e.target.value.trim().toLowerCase();renderModels();};
$('#copy').onclick=async()=>{try{await navigator.clipboard.writeText(config.api.base_url);toast('API 地址已复制');}catch{toast('无法访问剪贴板，请选中 API 地址手动复制');}};
function close(){if(config.announcement.policy==='once')storage.set(noticeKey(),'once');$('#notice-dialog').close();}
$('#notice-close').onclick=close;$('#notice-dialog').addEventListener('cancel',e=>{e.preventDefault();close();});
$('#notice-today').onclick=()=>{storage.set(noticeKey(),day());$('#notice-dialog').close();};$('#notice-never').onclick=()=>{storage.set(noticeKey(),'never');$('#notice-dialog').close();};$('#notice-open').onclick=()=>$('#notice-dialog').showModal();
// Draft previews arrive through a one-use channel from the local editor. The
// normal public page never reads a draft endpoint or browser-persisted config.
const previewId=new URL(location.href).searchParams.get('preview');
async function load(){
  if(previewId&&/^[\da-f-]{36}$/.test(previewId)) {
    $('#load-status').hidden=false;$('#load-status').textContent='草稿预览 · 等待编辑器发送配置';
    const ch=new BroadcastChannel('relay-preview-'+previewId);const timeout=setTimeout(()=>{ch.close();$('#load-status').textContent='预览已过期，请回到编辑器重新打开。';},10000);
    ch.onmessage=e=>{try{render(validateConfig(e.data));$('#load-status').textContent='草稿预览 · 内容尚未发布';clearTimeout(timeout);ch.close();}catch{$('#load-status').textContent='草稿配置校验失败';}};ch.postMessage('ready');return;
  }
  try{const snap=await getJSON('/homepage/config.json');if(snap.schema_version!==1||!Number.isSafeInteger(snap.revision)||snap.revision<1)throw new Error('版本错误');const c=validateConfig(snap.config);
    if(c.runtime.system_settings){try{const s=readEnvelope(await getJSON('/api/v1/settings/public'));if(typeof s.site_name==='string'&&s.site_name.trim()&&s.site_name.length<=80)c.brand.name=s.site_name;if(safeHref(s.api_base_url,{absolute:true}))c.api.base_url=s.api_base_url;if(s.registration_enabled===false)c.actions=c.actions.filter(a=>a.href!=='/register');}catch{c.api.hint+=' · 使用已发布地址快照。';}}
    render(c);
  }catch{$('#load-status').hidden=false;$('#load-status').textContent='首页配置暂时无法载入。你仍可登录控制台，或稍后刷新页面。';$('#hero-description').textContent='统一管理密钥、额度和调用记录。';$('#api-section').hidden=true;$('#models').hidden=true;$('#capabilities').hidden=true;}
}
load();
