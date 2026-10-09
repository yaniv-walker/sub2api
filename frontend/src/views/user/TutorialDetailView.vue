<template>
  <AppLayout>
    <div v-if="loading" class="grid min-h-[36rem] place-items-center"><div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div></div>
    <div v-else-if="error" class="card p-10 text-center"><p class="text-red-500">{{ error }}</p><button class="btn btn-secondary mt-4" @click="load">返回重试</button></div>
    <div v-else-if="tutorial" class="space-y-5">
      <div class="text-sm text-gray-500"><button class="hover:text-primary-600" @click="router.push('/tutorial')">教程与售后</button>　/　{{ tutorial.title }}</div>
      <header class="flex flex-col gap-4 border-b border-gray-200 pb-6 dark:border-dark-700 md:flex-row md:items-start md:justify-between"><div><p class="text-xs font-semibold text-orange-600">{{ categoryLabel(tutorial.category) }}</p><h1 class="mt-2 text-3xl font-bold text-gray-900 dark:text-white">{{ tutorial.title }}</h1><p class="mt-3 max-w-3xl text-sm leading-6 text-gray-500 dark:text-gray-400">{{ tutorial.summary }}</p><p class="mt-3 text-xs text-gray-400">更新于 {{ formatDate(tutorial.updated_at) }}</p></div><div class="flex gap-2"><button class="btn btn-secondary" @click="copyLink">▣ 复制链接</button><button v-if="authStore.isAdmin" class="btn btn-primary" @click="router.push('/admin/tutorials')">编辑本页</button></div></header>
      <div class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_15rem]"><article class="card min-w-0 px-5 py-7 sm:px-8" v-html="rendered"></article><aside v-if="toc.length" class="hidden lg:block"><div class="sticky top-24 border-l-2 border-gray-200 pl-4 dark:border-dark-700"><p class="mb-3 text-xs font-semibold uppercase text-gray-400">本页目录</p><button v-for="item in toc" :key="item.id" class="block w-full py-1 text-left text-xs text-gray-500 hover:text-primary-600" :class="item.level>2?'pl-3':''" @click="scrollTo(item.id)">{{ item.text }}</button></div></aside></div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import AppLayout from '@/components/layout/AppLayout.vue'
import { getTutorial, type Tutorial } from '@/api/tutorial'
import { useAuthStore } from '@/stores/auth'

const route=useRoute();const router=useRouter();const authStore=useAuthStore();const tutorial=ref<Tutorial|null>(null);const loading=ref(true);const error=ref('')
const toc=ref<{id:string;text:string;level:number}[]>([])
const rendered=computed(()=>{if(!tutorial.value)return '';const html=DOMPurify.sanitize(tutorial.value.content_html||marked.parse(tutorial.value.content_markdown||'') as string,{ADD_TAGS:['video','source'],ADD_ATTR:['controls','preload','playsinline','poster','src','data-width']});let i=0;return html.replace(/<(h[1-4])>(.*?)<\/h[1-4]>/gi,(_,tag:string,content:string)=>{const text=content.replace(/<[^>]+>/g,'').trim();const id=`tutorial-heading-${i++}`;toc.value.push({id,text,level:Number(tag[1])});return `<${tag} id="${id}">${content}</${tag}>`})})
const categoryLabel=(v:string)=>({ 'getting-started':'开始使用',clients:'客户端接入',billing:'充值与订阅',troubleshooting:'问题排查'}[v]||v);const formatDate=(v:string)=>new Intl.DateTimeFormat('zh-CN',{year:'numeric',month:'long',day:'numeric'}).format(new Date(v))
async function load(){loading.value=true;error.value='';toc.value=[];try{tutorial.value=await getTutorial(String(route.params.slug));document.title=`${tutorial.value.title} - 教程与售后`}catch(e:any){error.value=e?.message||'教程加载失败'}finally{loading.value=false}}
function scrollTo(id:string){document.getElementById(id)?.scrollIntoView({behavior:'smooth',block:'start'})};async function copyLink(){try{await navigator.clipboard.writeText(location.href)}catch{}}
onMounted(load)
</script>

<style scoped>
:deep(article h1),:deep(article h2),:deep(article h3),:deep(article h4){scroll-margin-top:90px}:deep(article h1){font-size:2rem;font-weight:700;margin-bottom:1.3rem}:deep(article h2){font-size:1.4rem;font-weight:700;margin:2rem 0 .75rem}:deep(article h3){font-size:1.1rem;font-weight:650;margin:1.4rem 0 .5rem}:deep(article p),:deep(article ul),:deep(article ol){margin:.85rem 0;line-height:1.85;font-size:.95rem}:deep(article ul){list-style:disc;padding-left:1.4rem}:deep(article ol){list-style:decimal;padding-left:1.4rem}:deep(article img),:deep(article video){display:block;max-width:100%;margin:1rem auto;border-radius:.7rem}:deep(article video){width:100%;background:#0f172a;min-height:18rem}:deep(article a){color:#0284c7;text-decoration:underline}
</style>
