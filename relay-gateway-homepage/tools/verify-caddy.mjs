// Local-only smoke test with a disposable Caddy container and dummy upstream.
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import assert from 'node:assert/strict';
import {projectRoot,exportRelease} from './export.mjs';
import {Store} from '../lib/store.mjs';

const seed=JSON.parse(fs.readFileSync(path.join(projectRoot,'public/homepage/config.json'),'utf8'));
const release=exportRelease(new Store(path.join(projectRoot,'data/state.json'),seed));
const artifactDir=path.join(projectRoot,'artifacts');fs.mkdirSync(artifactDir,{recursive:true});
const name='relay-home-smoke-'+randomUUID().slice(0,8);
const upstream=http.createServer((req,res)=>{res.setHeader('Content-Type','text/plain');res.end('UPSTREAM '+req.url);});
await new Promise(r=>upstream.listen(4190,'127.0.0.1',r));
const config=fs.readFileSync(path.join(projectRoot,'deploy/Caddyfile.example'),'utf8').replace('relay-gateway.xyz {',':80 {').replace('reverse_proxy 127.0.0.1:8080','reverse_proxy host.docker.internal:4190');
const file=path.join(artifactDir,'Caddyfile.smoke');fs.writeFileSync(file,config);
const docker=(args)=>execFileSync('docker',args,{encoding:'utf8',timeout:30000});
let started=false;
try {
  docker(['run','--rm','-d','--name',name,'-p','127.0.0.1:4188:80','--mount',`type=bind,source=${artifactDir},target=/test,readonly`,'--mount',`type=bind,source=${release.path},target=/srv/sub2api-homepage/current,readonly`,'caddy:2.6.2','caddy','run','--config','/test/Caddyfile.smoke','--adapter','caddyfile']);started=true;
  const base='http://127.0.0.1:4188';
  for(let i=0;i<30;i++){try{const r=await fetch(base,{signal:AbortSignal.timeout(800)});if(r.ok)break;}catch{}await new Promise(r=>setTimeout(r,100));}
  const root=await fetch(base);assert.equal(root.status,200);assert.match(await root.text(),/homepage-assets\/home.mjs/);assert.equal(root.headers.get('cache-control'),'no-cache');
  const c=await fetch(base+'/homepage/config.json');assert.equal(c.status,200);assert.equal((await c.json()).revision,release.revision);
  const module=await fetch(base+'/homepage-assets/home.mjs');assert.equal(module.status,200);assert.match(module.headers.get('content-type'),/javascript/);
  for(const p of ['/login','/register','/dashboard','/assets/test.js','/v1/models','/api/v1/settings/public','/admin-api/state','/auth/callback']){const r=await fetch(base+p);assert.equal(await r.text(),'UPSTREAM '+p);}
  assert.equal((await fetch(base+'/homepage-assets/missing.js')).status,404);
  console.log('Caddy 2.6.2 smoke PASS: exact root, config, assets, missing-asset 404; login/API/gateway/assets/callback paths preserve upstream.');
}finally {
  if(started)docker(['stop',name]);
  await new Promise(r=>upstream.close(r));
}
