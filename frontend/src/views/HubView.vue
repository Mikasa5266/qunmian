<template>
  <section class="hub-page">
    <div class="page-head">
      <h1>学生端群面工作台</h1>
      <p>邀请同伴、追踪群面状态并进入独立群面空间。</p>
    </div>

    <div class="hub-grid">
      <article class="panel-card">
        <h2>发起群面邀请</h2>
        <label>
          房间名称
          <input v-model.trim="roomName" placeholder="例如：校招实习 · 综合面" />
        </label>
        <button class="primary-btn" @click="handleCreateInvite" :disabled="loadingCreate">
          {{ loadingCreate ? "生成中..." : "发起群面邀请" }}
        </button>
        <div v-if="createdInvite" class="invite-result">
          <p>邀请码：<strong>{{ createdInvite.inviteCode }}</strong></p>
          <p>邀请链接：{{ createdInvite.inviteLink }}</p>
          <button class="secondary-btn" @click="copyInvite(createdInvite.inviteLink)">复制邀请链接</button>
          <button class="secondary-btn" @click="goRoom(createdInvite.roomId)">进入群面房间</button>
        </div>
      </article>

      <article class="panel-card">
        <h2>通过邀请码加入</h2>
        <label>
          邀请码
          <input v-model.trim="joinCode" placeholder="请输入邀请码" />
        </label>
        <button class="primary-btn" @click="handleJoin" :disabled="loadingJoin">{{ loadingJoin ? "加入中..." : "加入群面" }}</button>
        <p class="info-text">至少 3 位面试者加入后，任意成员可开始群面。</p>
      </article>
    </div>

    <article class="panel-card room-list-card">
      <div class="row-between">
        <h2>我的群面房间</h2>
        <button class="ghost-btn" @click="loadRooms">刷新</button>
      </div>
      <div class="room-list" v-if="rooms.length">
        <div class="room-item" v-for="room in rooms" :key="room.roomId">
          <div>
            <h3>{{ room.name }}</h3>
            <p>
              邀请码 {{ room.inviteCode }} · {{ room.participants.length }}/{{ room.maxParticipants }} 人
              · {{ room.started ? "进行中" : "未开始" }}
            </p>
          </div>
          <button class="secondary-btn" @click="goRoom(room.roomId)">进入房间</button>
        </div>
      </div>
      <p class="info-text" v-else>你还没有加入任何群面房间。</p>
    </article>

    <p class="error-text" v-if="errorText">{{ errorText }}</p>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { acceptInvite, createInvite, myRooms, type RoomState } from "../api";

const router = useRouter();
const route = useRoute();

const roomName = ref("校招实习 · 综合面");
const joinCode = ref("");
const loadingCreate = ref(false);
const loadingJoin = ref(false);
const errorText = ref("");
const rooms = ref<RoomState[]>([]);
const createdInvite = ref<{ roomId: string; roomName: string; inviteCode: string; inviteLink: string } | null>(null);

async function loadRooms(): Promise<void> {
  try {
    const data = await myRooms();
    rooms.value = data.rooms;
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : "加载房间失败";
  }
}

async function handleCreateInvite(): Promise<void> {
  loadingCreate.value = true;
  errorText.value = "";
  try {
    createdInvite.value = await createInvite(roomName.value || "群面房间");
    await loadRooms();
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : "创建邀请失败";
  } finally {
    loadingCreate.value = false;
  }
}

async function handleJoin(): Promise<void> {
  if (!joinCode.value) {
    errorText.value = "请输入邀请码";
    return;
  }
  loadingJoin.value = true;
  errorText.value = "";
  try {
    const result = await acceptInvite(joinCode.value);
    await loadRooms();
    await router.push(`/room/${result.room.roomId}`);
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : "加入失败";
  } finally {
    loadingJoin.value = false;
  }
}

function goRoom(roomId: string): void {
  void router.push(`/room/${roomId}`);
}

async function copyInvite(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    errorText.value = "复制失败，请手动复制链接";
  }
}

onMounted(async () => {
  await loadRooms();

  const invite = String(route.query.invite || "").trim();
  if (invite) {
    joinCode.value = invite;
    await handleJoin();
  }
});
</script>
