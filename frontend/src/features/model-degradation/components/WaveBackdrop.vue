<script setup lang="ts">
/**
 * 页面背景波线。
 *
 * 参考实现里是三组错位的正弦曲线，透明度与振幅递减，形成纵深。
 * 纯装饰，因此 aria-hidden 且不接收任何 props —— 它不该有能力影响内容。
 */
</script>

<template>
  <svg class="ma-wave" viewBox="0 0 1440 320" preserveAspectRatio="none" aria-hidden="true">
    <path
      class="ma-wave-line ma-wave-line-far"
      d="M0 208 C 180 168 300 248 480 208 C 660 168 780 248 960 208 C 1140 168 1260 248 1440 208"
    />
    <path
      class="ma-wave-line ma-wave-line-mid"
      d="M0 232 C 200 192 320 272 520 232 C 720 192 840 272 1040 232 C 1240 192 1320 272 1440 232"
    />
    <path
      class="ma-wave-line ma-wave-line-near"
      d="M0 256 C 220 216 340 296 560 256 C 780 216 900 296 1120 256 C 1300 220 1360 290 1440 258"
    />
  </svg>
</template>

<style scoped>
.ma-wave {
  position: absolute;
  inset: auto 0 0 0;
  width: 100%;
  height: 210px;
  pointer-events: none;
  z-index: 0;
}

.ma-wave-line {
  fill: none;
  stroke-linecap: round;
  /* 曲线缓慢漂移，暗示「一直在监测」。 */
  animation: ma-wave-drift 9s ease-in-out infinite;
}

.ma-wave-line-far {
  stroke: var(--ma-primary);
  stroke-width: 1;
  opacity: 0.1;
  animation-delay: -2s;
}

.ma-wave-line-mid {
  stroke: var(--ma-primary);
  stroke-width: 1.2;
  opacity: 0.14;
  animation-delay: -4s;
}

.ma-wave-line-near {
  stroke: var(--ma-pass);
  stroke-width: 1.4;
  opacity: 0.18;
  animation-delay: -6s;
}

@keyframes ma-wave-drift {
  0%,
  100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(-6px);
  }
}

/* 尊重系统的「减少动效」偏好：装饰动画不该成为障碍。 */
@media (prefers-reduced-motion: reduce) {
  .ma-wave-line {
    animation: none;
  }
}
</style>
