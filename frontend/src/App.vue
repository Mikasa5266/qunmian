<template>
  <div class="app-root">
    <MainNav v-if="showNav" />
    <main class="page-content" :class="{ withNav: showNav }">
      <router-view />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import MainNav from "./components/MainNav.vue";
import { isAuthed } from "./auth";

const route = useRoute();
const showNav = computed(() => route.path !== "/login" && isAuthed());
</script>
