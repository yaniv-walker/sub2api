import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';
import {Store} from '../lib/store.mjs';
import {validateConfig,modelCandidates,readEnvelope} from '../public/homepage-assets/contract.mjs';
import {projectRoot,exportRelease} from './export.mjs';

const mime={'.html':'text/html; charset=utf-8','.css':'text/css; charset=utf-8','.mjs':'application/javascript; charset=utf-8','.json':'application/json; charset=utf-8','.svg':'image/svg+xml','.png':'image/png','.webp':'image/webp','.jpg':'image/jpeg','.jpeg':'image/jpeg'};
const csp="default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'";
export function createApp({store,live=false}={}) {
  const seed=JSON.parse(fs.readFileSync(path.join(projectRoot,'public','homepage','config.json'),'utf8'));
  store??=new Store(path.join(projectRoot,'data','state.json'),seed);
  function json(res,status,data,cache='no-store') {res.writeHead(status,{'Content-Type':mime['.json'],'Cache-Control':cache});res.end(JSON.stringify(data));}
  async function body(req) {
    if(!req.headers['content-type']?.startsWith('application/json')) {const e=new Error('请求需使用 application/json');e.status=415;throw e;}
    let size=0,parts=[];
    for await(const chunk of req){size+=chunk.length;if(size>300000){const e=new Error('配置请求过大');e.status=413;throw e;}parts.push(chunk);}
    try{return JSON.parse(Buffer.concat(parts).toString('utf8'));}catch{const e=new Error('JSON 格式无效');e.status=400;throw e;}
  }
  async function source(endpoint) {
    if(!live) {
      if(endpoint==='settings/public') return {code:0,message:'success',data:{site_name:'RelayGateway',api_base_url:'https://relay-gateway.xyz/v1',registration_enabled:true}};
      return {code:0,message:'success',data:{groups:[{is_exclusive:false,models:seed.config.models.items.map(m=>({name:m.id}))}]}};
    }
    // Only two anonymous read-only endpoints. No credentials, cookies or TLS bypass.
    const r=await fetch(`https://relay-gateway.xyz/api/v1/${endpoint}`,{signal:AbortSignal.timeout(6000),redirect:'error',headers:{Accept:'application/json'}});
    if(!r.ok) throw new Error(`公开源不可用 (${r.status})`);
    const raw=await r.text();if(raw.length>2000000) throw new Error('公开源响应过大');return JSON.parse(raw);
  }
  return http.createServer(async(req,res)=>{
    res.setHeader('Content-Security-Policy',csp);res.setHeader('X-Content-Type-Options','nosniff');res.setHeader('Referrer-Policy','same-origin');res.setHeader('X-Frame-Options','DENY');
    try {
      const host=req.headers.host;const port=req.socket.localPort;
      if(![`127.0.0.1:${port}`,`localhost:${port}`].includes(host)) return json(res,403,{error:{code:'INVALID_HOST',message:'仅允许本机 Host'}});
      const origin=`http://${host}`;
      if(req.headers.origin && req.headers.origin!==origin) return json(res,403,{error:{code:'INVALID_ORIGIN',message:'跨域访问被拒绝'}});
      const url=new URL(req.url,origin);const p=url.pathname;
      if(!['GET','HEAD','POST','PUT'].includes(req.method)) return json(res,405,{error:{code:'METHOD_NOT_ALLOWED',message:'方法不支持'}});
      if(['POST','PUT'].includes(req.method)&&req.headers.origin!==origin) return json(res,403,{error:{code:'ORIGIN_REQUIRED',message:'写入需来自本机配置页'}});
      if(p==='/homepage/config.json'&&['GET','HEAD'].includes(req.method)) {
        const data=store.public();const etag='"'+createHash('sha256').update(JSON.stringify(data)).digest('hex').slice(0,20)+'"';res.setHeader('ETag',etag);
        if(req.headers['if-none-match']===etag){res.writeHead(304,{'Cache-Control':'no-cache'});return res.end();}
        return json(res,200,data,'no-cache');
      }
      if(p==='/admin-api/state'&&req.method==='GET') return json(res,200,{data:store.read(),source:live?'live':'fixture'});
      if(p==='/admin-api/candidates'&&req.method==='GET') return json(res,200,{data:modelCandidates(await source('model-plaza')),source:live?'live':'fixture',observed_at:new Date().toISOString()});
      if(p==='/api/v1/settings/public'&&req.method==='GET') return json(res,200,await source('settings/public'));
      if(p==='/api/v1/model-plaza'&&req.method==='GET') return json(res,200,await source('model-plaza'));
      if(p.startsWith('/admin-api/')&&['POST','PUT'].includes(req.method)) {
        const v=await body(req);
        if(p==='/admin-api/draft'&&req.method==='PUT') return json(res,200,{data:store.save(v.config,v.expected_revision)});
        if(p==='/admin-api/validate'&&req.method==='POST') return json(res,200,{data:validateConfig(v.config)});
        if(p==='/admin-api/publish'&&req.method==='POST') return json(res,200,{data:store.publish(v.expected_revision)});
        if(p==='/admin-api/restore'&&req.method==='POST') return json(res,200,{data:store.restore(v.revision,v.expected_revision)});
        if(p==='/admin-api/export'&&req.method==='POST') return json(res,200,{data:exportRelease(store)});
      }
      if(p==='/login'||p==='/register'||p==='/dashboard'||p==='/model-plaza') {res.writeHead(302,{Location:`https://relay-gateway.xyz${p}`,'Cache-Control':'no-store'});return res.end();}
      let file;
      if(req.method==='GET'||req.method==='HEAD') {
        if(p==='/')file=path.join(projectRoot,'public','index.html');
        if(p==='/admin'||p==='/admin/')file=path.join(projectRoot,'admin','index.html');
        if(/^\/(homepage-assets|admin-assets)\/[a-zA-Z0-9_./-]+\.(mjs|css|svg|png|webp|jpe?g)$/.test(p)&&!p.includes('..')) file=path.join(projectRoot,p.startsWith('/admin-assets')?'admin':'public',p.slice(1));
      }
      if(file&&fs.existsSync(file)){res.writeHead(200,{'Content-Type':mime[path.extname(file)]||'application/octet-stream','Cache-Control':'no-cache'});return res.end(req.method==='HEAD'?undefined:fs.readFileSync(file));}
      return json(res,404,{error:{code:'NOT_FOUND',message:'路径不存在'}});
    }catch(e){json(res,e.status||500,{error:{code:e.code||'REQUEST_FAILED',message:e.status?e.message:'操作失败；公开数据源或磁盘可能不可用'}});}
  });
}
if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  const port=Number(process.env.HOMEPAGE_PORT||4178);
  createApp({live:process.argv.includes('--live-public')}).listen(port,'127.0.0.1',()=>console.log(`首页 http://127.0.0.1:${port}/\n配置 http://127.0.0.1:${port}/admin\n公开数据源 ${process.argv.includes('--live-public')?'线上匿名只读':'离线联调 fixture'}`));
}
