<template>
  <header class="main-nav">
    <div class="main-nav-inner">
      <div class="brand-wrap">
        <div class="brand-avatar">◌</div>
        <div class="brand-text">
          <strong>智聘AI</strong>
          <span>学生端</span>
        </div>
      </div>

      <nav class="nav-menu">
        <a v-for="item in navItems" :key="item.label" class="nav-item" href="javascript:void(0)">
          <span class="nav-dot" :class="item.icon"></span>
          {{ item.label }}
        </a>
      </nav>

      <div class="user-area">
        <div class="user-avatar">•</div>
        <span>{{ userName }}</span>
        <span class="caret">⌄</span>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { authState } from "../auth";

const navItems = [
  { label: "首页", icon: "icon-home" },
  { label: "简历解析", icon: "icon-file" },
  { label: "刷题", icon: "icon-book" },
  { label: "模拟面试", icon: "icon-video" },
  { label: "复盘报告", icon: "icon-chart" },
  { label: "校友社区", icon: "icon-users" }
];
const userName = computed(() => authState.user?.username || "student02");
</script>
