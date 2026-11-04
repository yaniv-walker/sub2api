<script setup lang="ts">
/**
 * 吉祥物。
 *
 * 参考实现用一只「狗 / 猴」双形态的拟人小动物表达状态：
 *   thinking 思考中（转眼睛、冒汗）
 *   pass     通过（眯眼笑、举爪）
 *   fail     失败（耷拉耳朵、皱眉）
 *
 * 它是纯装饰性的情绪表达，不承载任何信息 —— 因此 aria-hidden，
 * 真正的内容由旁边的文字负责。这样读屏用户不会读到「一只狗」。
 */
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 动物形态。 */
    animal?: 'dog' | 'monkey'
    /** 情绪状态。 */
    state?: 'thinking' | 'pass' | 'fail'
  }>(),
  {
    animal: 'dog',
    state: 'thinking'
  }
)

const isMonkey = computed(() => props.animal === 'monkey')
const isPass = computed(() => props.state === 'pass')
const isFail = computed(() => props.state === 'fail')

/** 根节点类：驱动配色与局部动画。 */
const rootClass = computed(() => ({
  'mc-root': true,
  'is-dog': !isMonkey.value,
  'is-monkey': isMonkey.value,
  'is-pass': isPass.value,
  'is-fail': isFail.value
}))

/** 眼睛：通过时眯眼，失败时下垂，思考时正常。 */
const eyeClass = computed(() => ({
  'mc-eye': true,
  'mc-eye-happy': isPass.value,
  'mc-eye-squint': isFail.value
}))
</script>

<template>
  <svg :class="rootClass" viewBox="0 0 120 120" aria-hidden="true">
    <!-- 尾巴：狗是上翘，猴是长卷 -->
    <path
      class="mc-tail"
      :d="isMonkey ? 'M88 84 C 104 84 110 70 104 62 C 100 56 92 58 92 64' : 'M86 82 C 100 78 104 64 96 58'"
    />

    <!-- 耳朵 -->
    <ellipse v-if="!isMonkey" class="mc-ear mc-ear-l" cx="34" cy="38" rx="10" ry="14" />
    <ellipse v-if="!isMonkey" class="mc-ear mc-ear-r" cx="86" cy="38" rx="10" ry="14" />
    <circle v-if="isMonkey" class="mc-ear mc-ear-l" cx="26" cy="42" r="12" />
    <circle v-if="isMonkey" class="mc-ear mc-ear-r" cx="94" cy="42" r="12" />

    <!-- 身体 -->
    <g class="mc-body-group">
      <ellipse class="mc-body" cx="60" cy="74" rx="26" ry="22" />
      <ellipse class="mc-fur mc-fur-soft" cx="60" cy="78" rx="16" ry="13" />
    </g>

    <!-- 头 -->
    <ellipse class="mc-head" cx="60" cy="48" rx="30" ry="27" />
    <!-- 面部浅色区 -->
    <ellipse class="mc-mark mc-mark-dim" cx="60" cy="54" rx="20" ry="17" />

    <!-- 眼睛 -->
    <g :class="eyeClass">
      <ellipse class="mc-eye-l" cx="48" cy="46" rx="4.2" ry="4.8" />
      <ellipse class="mc-eye-r" cx="72" cy="46" rx="4.2" ry="4.8" />
      <!-- 高光：让它看起来有神。 -->
      <circle class="mc-dot mc-dot-2" cx="49.6" cy="44.4" r="1.4" />
      <circle class="mc-dot mc-dot-3" cx="73.6" cy="44.4" r="1.4" />
    </g>

    <!-- 鼻子与嘴 -->
    <ellipse class="mc-nose" cx="60" cy="56" rx="3.4" ry="2.6" />
    <path class="mc-mouth" :d="isPass ? 'M54 62 Q 60 68 66 62' : isFail ? 'M54 66 Q 60 61 66 66' : 'M56 63 Q 60 66 64 63'" />

    <!-- 领巾 -->
    <path class="mc-collar" d="M44 72 Q 60 80 76 72 L 76 78 Q 60 86 44 78 Z" />

    <!-- 名牌 -->
    <rect class="mc-tag" x="54" y="78" width="12" height="8" rx="2" />

    <!-- 爪子 -->
    <g class="mc-paw">
      <ellipse cx="42" cy="94" rx="7" ry="5" />
      <ellipse cx="78" cy="94" rx="7" ry="5" />
      <circle class="mc-paw-sole" cx="42" cy="94" r="2.2" />
      <circle class="mc-paw-sole" cx="78" cy="94" r="2.2" />
    </g>

    <!-- 思考中的汗滴 -->
    <path v-if="state === 'thinking'" class="mc-sweat" d="M90 30 q 4 6 0 9 q -4 -3 0 -9" />

    <!-- 通过时的星星 -->
    <g v-if="isPass">
      <path class="mc-spark" d="M96 34 l2 4 4 2 -4 2 -2 4 -2 -4 -4 -2 4 -2 z" />
      <path class="mc-spark mc-spark-2" d="M24 30 l1.4 3 3 1.4 -3 1.4 -1.4 3 -1.4 -3 -3 -1.4 3 -1.4 z" />
    </g>

    <!-- 思考时的问号气泡 -->
    <g v-if="state === 'thinking'" class="mc-think">
      <circle class="mc-dot" cx="98" cy="46" r="2" />
      <circle class="mc-dot" cx="102" cy="40" r="1.4" />
    </g>
  </svg>
</template>

<style scoped>
.mc-root {
  width: 76px;
  height: 76px;
  flex: 0 0 auto;
}

/* 基础配色：狗偏暖棕，猴偏灰褐。 */
.mc-root.is-dog .mc-head,
.mc-root.is-dog .mc-body,
.mc-root.is-dog .mc-ear,
.mc-root.is-dog .mc-tail,
.mc-root.is-dog .mc-paw {
  fill: #d9b98f;
}

.mc-root.is-monkey .mc-head,
.mc-root.is-monkey .mc-body,
.mc-root.is-monkey .mc-ear,
.mc-root.is-monkey .mc-tail,
.mc-root.is-monkey .mc-paw {
  fill: #b79a7c;
}

.mc-mark-dim {
  fill: #f4e8d8;
}

.mc-root.is-monkey .mc-mark-dim {
  fill: #e8d8c4;
}

.mc-nose {
  fill: #6b5340;
}

.mc-eye-l,
.mc-eye-r {
  fill: #3b2a20;
}

.mc-dot {
  fill: #fff;
}

.mc-dot-2,
.mc-dot-3 {
  opacity: 0.9;
}

.mc-mouth {
  fill: none;
  stroke: #6b5340;
  stroke-width: 1.6;
  stroke-linecap: round;
}

.mc-collar {
  fill: var(--ma-primary);
  opacity: 0.85;
}

.mc-tag {
  fill: #e4bc50;
}

.mc-paw-sole {
  fill: #b08a63;
  opacity: 0.6;
}

.mc-sweat {
  fill: #5a96c8;
  opacity: 0.8;
}

.mc-spark {
  fill: #e4bc50;
}

.mc-spark-2 {
  opacity: 0.7;
}

.mc-think .mc-dot {
  fill: var(--ma-muted);
}

/* 通过：眯眼笑。用 transform 压缩纵向高度模拟「笑成一条缝」。 */
.mc-eye-happy .mc-eye-l,
.mc-eye-happy .mc-eye-r {
  transform: scaleY(0.35);
  transform-box: fill-box;
  transform-origin: center;
}

.mc-eye-happy .mc-dot-2,
.mc-eye-happy .mc-dot-3 {
  display: none;
}

/* 失败：眼睛下垂，并把高光藏掉（失神）。 */
.mc-eye-squint .mc-eye-l {
  transform: rotate(-12deg) scaleY(0.8);
  transform-box: fill-box;
  transform-origin: center;
}

.mc-eye-squint .mc-eye-r {
  transform: rotate(12deg) scaleY(0.8);
  transform-box: fill-box;
  transform-origin: center;
}

.mc-eye-squint .mc-dot-2,
.mc-eye-squint .mc-dot-3 {
  opacity: 0.3;
}

/* 轻微呼吸，让静态图不死板。 */
.mc-body-group {
  transform-origin: 60px 74px;
  animation: mc-breathe 3.6s ease-in-out infinite;
}

@keyframes mc-breathe {
  0%,
  100% {
    transform: scale(1);
  }
  50% {
    transform: scale(1.02);
  }
}

@media (prefers-reduced-motion: reduce) {
  .mc-body-group {
    animation: none;
  }
}
</style>
