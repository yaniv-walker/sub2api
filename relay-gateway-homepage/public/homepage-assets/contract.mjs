// The editor, exporter and public page share the same contract. Unknown fields
// are rejected before public content can be exported.
export class ConfigError extends Error {
  constructor(path, message) { super(`${path}: ${message}`); this.code = 'VALIDATION_ERROR'; this.status = 422; }
}
const fail = (p, m) => { throw new ConfigError(p, m); };
const qqHosts = new Set(['qm.qq.com','qun.qq.com','jq.qq.com','shang.qq.com']);
function obj(v, keys, p) {
  if (!v || typeof v !== 'object' || Array.isArray(v)) fail(p, '必须是对象');
  for (const k of Object.keys(v)) if (!keys.includes(k)) fail(`${p}.${k}`, '未知字段');
  for (const k of keys) if (!(k in v)) fail(`${p}.${k}`, '缺少字段');
}
function str(v, p, max = 2000, empty = true) {
  if (typeof v !== 'string' || v.length > max || (!empty && !v.trim())) fail(p, `必须是${empty ? '' : '非空'}字符串，最多 ${max} 字符`);
  if (/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(v)) fail(p, '包含控制字符');
}
function bool(v,p) { if (typeof v !== 'boolean') fail(p,'必须是布尔值'); }
function choice(v, values, p) { if (!values.includes(v)) fail(p,`只能是 ${values.join(' / ')}`); }
export function normalizeHref(value) {
  if (typeof value !== 'string') return value;
  const trimmed = value.trim();
  // This is presentation unescaping, not URL decoding: preserve QQ tokens
  // (%2B, %2F, etc.) byte-for-byte and leave non-QQ backslashes invalid.
  return /^https?:\/\/(?:qm|qun|jq|shang)\.qq\.com(?:\/|$)/i.test(trimmed)
    ? trimmed.replaceAll('\\&', '&') : trimmed;
}
export function safeHref(value, { absolute = false, asset = false } = {}) {
  if (typeof value !== 'string' || !value) return false;
  // Pasted QQ invite URLs sometimes carry a trailing newline or encoded query
  // values such as %2F. Trim only the outside; reject whitespace/control bytes
  // inside the URL and reject encoded controls everywhere.
  const urlValue = normalizeHref(value);
  if (!urlValue || /[\s\\\u0000-\u001f\u007f]/.test(urlValue)) return false;
  if (/%(?:00|0[ad]|1[0-9a-f])/i.test(urlValue)) return false;
  if (asset) return /^\/homepage-assets\/[a-z0-9_./-]+$/i.test(urlValue) && !urlValue.includes('..');
  if (!absolute && /^#[a-z][\w-]*$/i.test(urlValue)) return true;
  if (!absolute && urlValue.startsWith('/') && !urlValue.startsWith('//')) {
    try { const u = new URL(urlValue, 'https://home.invalid'); return u.origin === 'https://home.invalid' && !/%(?:2f|5c)/i.test(u.pathname); } catch { return false; }
  }
  try {
    const u = new URL(urlValue);
    // HTTP is allowed only for known QQ invite domains because older QQ group
    // links are commonly generated as http://qm.qq.com/... . Arbitrary HTTP
    // links remain rejected; all other external links must use HTTPS.
    const secureExternal = u.protocol === 'https:';
    const qqHttp = !absolute && u.protocol === 'http:' && qqHosts.has(u.hostname.toLowerCase()) && !u.port;
    return (secureExternal || qqHttp) && !u.username && !u.password && !/%(?:2f|5c)/i.test(u.pathname);
  } catch { return false; }
}
function href(v,p, options) { if (!safeHref(v,options)) fail(p,'只允许安全站内路径、锚点或 HTTPS 外链；QQ 官方加群域名支持 HTTP（如 http://qm.qq.com/...），不支持其他 HTTP、javascript: 或 data:'); }
function list(v,p,max,fn) {
  if (!Array.isArray(v) || v.length > max) fail(p,`必须是数组，最多 ${max} 项`);
  const seen = new Set();
  v.forEach((x,i)=> { fn(x,`${p}[${i}]`); if (x.id) { if(seen.has(x.id)) fail(p,'ID 重复'); seen.add(x.id); } });
}
function action(a,p) {
  obj(a,['id','label','href','enabled','variant'],p);
  str(a.id,`${p}.id`,80,false); str(a.label,`${p}.label`,80,false);
  href(a.href,`${p}.href`); bool(a.enabled,`${p}.enabled`); choice(a.variant,['primary','secondary','text'],`${p}.variant`);
}
export function validateConfig(c) {
  obj(c,['schema_version','brand','hero','navigation','actions','api','models','capabilities','announcement','footer','runtime'], 'config');
  c = structuredClone(c);
  if(c.schema_version !== 1) fail('schema_version','仅支持版本 1');
  obj(c.brand,['name','logo_text','logo_url'],'brand');
  str(c.brand.name,'brand.name',80,false); str(c.brand.logo_text,'brand.logo_text',6,false); str(c.brand.logo_url,'brand.logo_url',300);
  if(c.brand.logo_url) href(c.brand.logo_url,'brand.logo_url',{asset:true});
  obj(c.hero,['badge','title','accent','description'],'hero');
  for(const k of Object.keys(c.hero)) str(c.hero[k],`hero.${k}`,k==='description'?1000:120,k==='accent');
  list(c.navigation,'navigation',12,action); list(c.actions,'actions',12,action);
  obj(c.api,['enabled','base_url','label','hint','copy_enabled'],'api');
  bool(c.api.enabled,'api.enabled'); href(c.api.base_url,'api.base_url',{absolute:true});
  str(c.api.label,'api.label',40,false); str(c.api.hint,'api.hint',500); bool(c.api.copy_enabled,'api.copy_enabled');
  obj(c.models,['enabled','title','description','observed_at','items'],'models');
  bool(c.models.enabled,'models.enabled'); str(c.models.title,'models.title',80,false); str(c.models.description,'models.description',600);
  str(c.models.observed_at,'models.observed_at',100);
  list(c.models.items,'models.items',200,(m,p)=> {
    obj(m,['id','label','category','enabled'],p); str(m.id,`${p}.id`,160,false); str(m.label,`${p}.label`,160,false);
    choice(m.category,['text','code','image','video'],`${p}.category`); bool(m.enabled,`${p}.enabled`);
  });
  list(c.capabilities,'capabilities',12,(f,p)=> {
    obj(f,['id','icon','title','description','enabled'],p); str(f.id,`${p}.id`,80,false);
    choice(f.icon,['layers','key','shield','bolt','globe','chart'],`${p}.icon`);
    str(f.title,`${p}.title`,80,false); str(f.description,`${p}.description`,1000); bool(f.enabled,`${p}.enabled`);
  });
  obj(c.announcement,['enabled','id','version','title','date','body','mode','policy','starts_at','ends_at','actions'],'announcement');
  const n=c.announcement;
  bool(n.enabled,'announcement.enabled'); str(n.id,'announcement.id',80,false);
  if(!Number.isSafeInteger(n.version)||n.version<1) fail('announcement.version','必须是正整数');
  str(n.title,'announcement.title',160,false); str(n.date,'announcement.date',40); str(n.body,'announcement.body',20000);
  choice(n.mode,['popup','banner','silent'],'announcement.mode'); choice(n.policy,['visit','daily','once'],'announcement.policy');
  for(const k of ['starts_at','ends_at']) if(n[k]!==null && (typeof n[k]!=='string'||!/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(n[k])||!Number.isFinite(Date.parse(n[k])))) fail(`announcement.${k}`,'必须为带时区的 ISO 日期或 null');
  if(n.starts_at&&n.ends_at&&Date.parse(n.starts_at)>=Date.parse(n.ends_at)) fail('announcement','结束时间必须晚于开始时间');
  list(n.actions,'announcement.actions',6,action);
  obj(c.footer,['copyright','links'],'footer'); str(c.footer.copyright,'footer.copyright',300); list(c.footer.links,'footer.links',12,action);
  obj(c.runtime,['system_settings'],'runtime'); bool(c.runtime.system_settings,'runtime.system_settings');
  if(JSON.stringify(c).length>250000) fail('config','配置超过大小限制');
  for (const action of [...c.navigation, ...c.actions, ...c.announcement.actions, ...c.footer.links]) {
    action.href = normalizeHref(action.href);
  }
  c.brand.logo_url = c.brand.logo_url.trim();
  c.api.base_url = c.api.base_url.trim();
  return structuredClone(c);
}
export function publicSnapshot(config,revision,publishedAt) {
  const c=validateConfig(config);
  c.navigation=c.navigation.filter(x=>x.enabled); c.actions=c.actions.filter(x=>x.enabled);
  c.models.items=c.models.items.filter(x=>x.enabled); c.capabilities=c.capabilities.filter(x=>x.enabled);
  if(!c.models.enabled) c.models={enabled:false,title:'模型目录',description:'',observed_at:'',items:[]};
  if(!c.api.enabled) c.api={enabled:false,base_url:'https://example.invalid/v1',label:'API BASE URL',hint:'',copy_enabled:false};
  c.footer.links=c.footer.links.filter(x=>x.enabled); c.announcement.actions=c.announcement.actions.filter(x=>x.enabled);
  if(!c.announcement.enabled) c.announcement={enabled:false,id:'disabled',version:1,title:'公告',date:'',body:'',mode:'silent',policy:'daily',starts_at:null,ends_at:null,actions:[]};
  return {schema_version:1,revision,published_at:publishedAt,config:c};
}
export function readEnvelope(v) {
  if(!v || v.code!==0 || !v.data || typeof v.data!=='object') throw new Error('Sub2API 响应必须为 {code:0,data:...}');
  return v.data;
}
export function modelCandidates(envelope) {
  const data=readEnvelope(envelope);
  if(!Array.isArray(data.groups)) throw new Error('模型广场缺少 groups');
  const result=new Map();
  for(const g of data.groups) {
    // Never import a private/exclusive group even if an unexpected source sends it.
    if(g.is_exclusive !== false || !Array.isArray(g.models)) continue;
    for(const m of g.models) if(typeof m.name==='string'&&m.name.length<=160&&m.name.trim()) {
      if(!result.has(m.name)) result.set(m.name,{id:m.name,label:m.name,category:'text',enabled:true});
    }
  }
  return [...result.values()].slice(0,200);
}
