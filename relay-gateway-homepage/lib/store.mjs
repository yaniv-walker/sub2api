import fs from 'node:fs';
import path from 'node:path';
import {randomUUID} from 'node:crypto';
import {validateConfig, publicSnapshot} from '../public/homepage-assets/contract.mjs';

export function atomicJSON(file,value) {
  fs.mkdirSync(path.dirname(file),{recursive:true});
  const tmp=`${file}.${randomUUID()}.tmp`;
  try {
    const fd=fs.openSync(tmp,'wx',0o600);
    try {fs.writeFileSync(fd,JSON.stringify(value,null,2)+'\n');fs.fsyncSync(fd);} finally {fs.closeSync(fd);}
    fs.renameSync(tmp,file);
  } finally { if(fs.existsSync(tmp)) fs.unlinkSync(tmp); }
}
export class Store {
  constructor(file,seed) {
    this.file=file;
    this.state=fs.existsSync(file)?JSON.parse(fs.readFileSync(file,'utf8')):{schema_version:1,draft_revision:1,draft:validateConfig(seed.config),published:{revision:seed.revision,published_at:seed.published_at,config:validateConfig(seed.config)},history:[]};
    validateConfig(this.state.draft);validateConfig(this.state.published.config);
  }
  read() { return structuredClone(this.state); }
  check(expected) {
    if(expected!==this.state.draft_revision) { const e=new Error('草稿已被其他操作更新，请重新载入');e.status=409;e.code='CONFIG_REVISION_CONFLICT';throw e; }
  }
  commit(next) {atomicJSON(this.file,next);this.state=next;return this.read();}
  save(config,expected) {
    this.check(expected);const next=this.read();next.draft=validateConfig(config);next.draft_revision++;return this.commit(next);
  }
  publish(expected) {
    this.check(expected);const next=this.read();
    next.history.unshift(next.published);next.history=next.history.slice(0,20);
    next.published={revision:next.published.revision+1,published_at:new Date().toISOString(),config:validateConfig(next.draft)};
    next.draft_revision++;return this.commit(next);
  }
  restore(revision,expected) {
    this.check(expected);const version=[this.state.published,...this.state.history].find(x=>x.revision===revision);
    if(!version) {const e=new Error('历史版本不存在');e.status=404;e.code='VERSION_NOT_FOUND';throw e;}
    return this.save(version.config,expected);
  }
  public() {const p=this.state.published;return publicSnapshot(p.config,p.revision,p.published_at);}
}
