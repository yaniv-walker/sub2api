import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {randomUUID,createHash} from 'node:crypto';
import {Store,atomicJSON} from '../lib/store.mjs';

export const projectRoot=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
export function exportRelease(store,root=projectRoot) {
  const snapshot=store.public();
  const id=`r${snapshot.revision}-${randomUUID().slice(0,8)}`;
  const dest=path.join(root,'dist',id);
  fs.mkdirSync(dest,{recursive:true});
  // Explicit allowlist: admin UI, state, source and logs never enter the static root.
  fs.copyFileSync(path.join(projectRoot,'public','index.html'),path.join(dest,'index.html'));
  function copyAssets(dir,relative=''){
    for(const entry of fs.readdirSync(dir,{withFileTypes:true})) {
      const name=relative+entry.name;
      if(entry.isSymbolicLink()||!/^[a-zA-Z0-9_./-]+$/.test(name)||name.includes('..')) throw new Error('静态资源路径不安全');
      if(entry.isDirectory()) copyAssets(path.join(dir,entry.name),name+'/');
      else {
        if(!/\.(mjs|css|svg|png|webp|jpe?g)$/.test(name)) throw new Error('非公开资源不能导出：'+name);
        const out=path.join(dest,'homepage-assets',name);fs.mkdirSync(path.dirname(out),{recursive:true});fs.copyFileSync(path.join(dir,entry.name),out);
      }
    }
  }
  copyAssets(path.join(projectRoot,'public','homepage-assets'));
  atomicJSON(path.join(dest,'homepage','config.json'),snapshot);
  const files={};
  function scan(dir){for(const e of fs.readdirSync(dir,{withFileTypes:true})){const f=path.join(dir,e.name);if(e.isDirectory())scan(f);else files[path.relative(dest,f).replaceAll('\\','/')]=createHash('sha256').update(fs.readFileSync(f)).digest('hex');}}
  scan(dest);
  atomicJSON(path.join(dest,'release-manifest.json'),{id,revision:snapshot.revision,files});
  return {id,revision:snapshot.revision,path:dest};
}
if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  const seed=JSON.parse(fs.readFileSync(path.join(projectRoot,'public','homepage','config.json'),'utf8'));
  console.log(JSON.stringify(exportRelease(new Store(path.join(projectRoot,'data','state.json'),seed)),null,2));
}
