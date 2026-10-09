<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div><p class="text-xs font-semibold uppercase tracking-[0.18em] text-primary-600">Help center</p><h1 class="mt-2 text-3xl font-bold text-gray-900 dark:text-white">从第一次配置，到问题排查</h1><p class="mt-2 text-sm text-gray-500 dark:text-gray-400">教程按使用场景整理，打开后即可跟着步骤完成配置。</p></div>
        <button class="btn btn-primary" @click="supportOpen = true">？ 联系售后</button>
      </header>
      <div class="flex flex-col gap-3 lg:flex-row">
        <label class="relative flex-1"><span class="pointer-events-none absolute left-3 top-2.5 text-gray-400">⌕</span><input v-model="query" class="input pl-9" placeholder="搜索客户端、配置步骤或错误" aria-label="搜索教程" /></label>
        <div class="flex gap-1 overflow-auto"><button v-for="item in categories" :key="item.value" class="shrink-0 rounded-lg px-3 py-2 text-sm font-medium" :class="category === item.value ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900' : 'text-gray-500 hover:bg-gray-100 dark:hover:bg-dark-700'" @click="category = item.value">{{ item.label }} <span class="opacity-60">{{ item.count }}</span></button></div>
      </div>
      <div v-if="loading" class="grid gap-3 md:grid-cols-2"><div v-for="n in 4" :key="n" class="h-48 animate-pulse rounded-lg bg-white dark:bg-dark-800"></div></div>
      <div v-else-if="error" class="card p-10 text-center"><p class="text-sm text-red-500">{{ error }}</p><button class="btn btn-secondary mt-4" @click="load">重试</button></div>
      <div v-else-if="filtered.length" class="grid gap-3 md:grid-cols-2"><button v-for="item in filtered" :key="item.id" class="card group text-left" @click="router.push(`/tutorial/${item.slug}`)"><div class="flex items-start justify-between"><span class="flex h-10 w-10 items-center justify-center rounded-lg bg-primary-50 text-primary-600 dark:bg-primary-900/20">▤</span><span class="text-gray-300 transition group-hover:translate-x-1 group-hover:text-primary-500">↗</span></div><p class="mt-5 text-xs font-semibold text-orange-600">{{ categoryLabel(item.category) }}</p><h2 class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ item.title }}</h2><p class="mt-2 line-clamp-3 text-sm leading-6 text-gray-500 dark:text-gray-400">{{ item.summary }}</p><p class="mt-5 border-t border-gray-100 pt-3 text-xs text-gray-400 dark:border-dark-700">更新于 {{ formatDate(item.updated_at) }}</p></button></div>
      <div v-else class="card py-14 text-center text-sm text-gray-500">没有匹配的教程</div>
    </div>
    <div v-if="supportOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4" @click.self="supportOpen = false"><div class="w-full max-w-2xl rounded-xl bg-white p-6 shadow-xl dark:bg-dark-800"><div class="flex items-start justify-between"><div><p class="text-xs font-semibold uppercase text-orange-600">After-sales support</p><h2 class="mt-1 text-2xl font-bold text-gray-900 dark:text-white">联系售后</h2><p class="mt-2 text-sm text-gray-500">扫码联系售后时，请备注注册邮箱或用户 ID。</p></div><button class="btn btn-secondary" @click="supportOpen = false">关闭</button></div><div class="mt-5 grid gap-3 sm:grid-cols-2"><div v-for="item in supportCards" :key="item.title" class="rounded-lg border border-gray-200 p-4 dark:border-dark-700"><h3 class="font-semibold text-gray-900 dark:text-white">{{ item.title }}</h3><p class="mt-1 text-xs text-gray-500">{{ item.desc }}</p><div class="mt-3 grid h-36 place-items-center rounded bg-gray-100 text-xs text-gray-400 dark:bg-dark-900">二维码待配置</div></div></div><p class="mt-4 text-xs text-gray-400">服务时间：工作日 12:00 - 24:00。</p></div></div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import { listTutorials, type Tutorial } from '@/api/tutorial'

const router = useRouter(); const query = ref(''); const category = ref('all'); const loading = ref(true); const error = ref(''); const items = ref<Tutorial[]>([]); const supportOpen = ref(false)
const categories = computed(() => [{value:'all',label:'全部教程',count:items.value.length},{value:'getting-started',label:'开始使用',count:items.value.filter(x=>x.category==='getting-started').length},{value:'clients',label:'客户端接入',count:items.value.filter(x=>x.category==='clients').length},{value:'troubleshooting',label:'问题排查',count:items.value.filter(x=>x.category==='troubleshooting').length}])
const filtered = computed(() => { const q=query.value.trim().toLowerCase(); return items.value.filter(x=>(category.value==='all'||x.category===category.value)&&(!q||`${x.title} ${x.summary}`.toLowerCase().includes(q))) })
const supportCards=[{title:'售后客服微信',desc:'适合安装配置、使用问题和日常咨询。'},{title:'售后客服 QQ',desc:'适合订单、支付和需要持续跟进的问题。'}]
const categoryLabel=(v:string)=>({ 'getting-started':'开始使用',clients:'客户端接入',billing:'充值与订阅',troubleshooting:'问题排查'}[v]||v)
const formatDate=(v:string)=>new Intl.DateTimeFormat('zh-CN',{year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date(v))
async function load(){loading.value=true;error.value='';try{const res=await listTutorials({page:1,page_size:100});items.value=res.items}catch(e:any){error.value=e?.message||'教程加载失败'}finally{loading.value=false}}
onMounted(load)
</script>
