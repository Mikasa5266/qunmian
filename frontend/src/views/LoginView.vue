<template>
  <section class="auth-page">
    <div class="auth-card">
      <h1>登录群面系统</h1>
      <p>支持注册和登录，登录后可创建邀请码并进入群面房间。</p>

      <div class="auth-tabs">
        <button :class="{ active: mode === 'login' }" @click="mode = 'login'">登录</button>
        <button :class="{ active: mode === 'register' }" @click="mode = 'register'">注册</button>
      </div>

      <form class="auth-form" @submit.prevent="submit">
        <label>
          用户名
          <input v-model.trim="username" minlength="3" required placeholder="例如 student01" />
        </label>
        <label>
          密码
          <input v-model="password" type="password" minlength="6" required placeholder="至少 6 位" />
        </label>

        <button class="primary-btn" type="submit" :disabled="loading">
          {{ loading ? "处理中..." : mode === "login" ? "登录" : "注册并登录" }}
        </button>
      </form>

      <p class="error-text" v-if="errorText">{{ errorText }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { login, register } from "../api";
import { setAuth } from "../auth";

const router = useRouter();
const mode = ref<"login" | "register">("login");
const username = ref("");
const password = ref("");
const loading = ref(false);
const errorText = ref("");

async function submit(): Promise<void> {
  loading.value = true;
  errorText.value = "";
  try {
    const result = mode.value === "login"
      ? await login(username.value, password.value)
      : await register(username.value, password.value);
    setAuth(result.token, result.user);
    await router.push("/hub");
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : "操作失败";
  } finally {
    loading.value = false;
  }
}
</script>
