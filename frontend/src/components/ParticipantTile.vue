<template>
  <div class="participant-tile" :class="{ offline: participant && !participant.online, self: isSelf }">
    <div class="tile-header" v-if="participant">
      <strong>{{ participant.username }}<em v-if="isSelf">（你）</em></strong>
      <span class="status-pill" :class="participant.online ? 'online' : 'offline'">
        {{ participant.online ? "在线" : "离线" }}
      </span>
      <span class="status-pill" :class="participant.muted ? 'muted' : 'speaking'">
        {{ participant.muted ? "已静音" : "可发言" }}
      </span>
    </div>

    <div class="tile-header" v-else>
      <strong>等待面试者加入</strong>
      <span class="status-pill offline">空位</span>
    </div>

    <div class="video-shell" v-if="participant">
      <video ref="videoRef" autoplay playsinline :muted="isSelf"></video>
      <div class="video-mask" v-if="!stream">{{ participant.username }}</div>
    </div>

    <div class="video-shell placeholder" v-else>
      <span>待加入</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import type { Participant } from "../api";

const props = defineProps<{
  participant: Participant | null;
  stream: MediaStream | null;
  isSelf: boolean;
}>();

const videoRef = ref<HTMLVideoElement | null>(null);

async function bindStream(): Promise<void> {
  if (!videoRef.value) {
    return;
  }
  videoRef.value.srcObject = props.stream;
  if (props.stream) {
    await nextTick();
    void videoRef.value.play().catch(() => undefined);
  }
}

onMounted(() => {
  void bindStream();
});

watch(
  () => props.stream,
  () => {
    void bindStream();
  }
);

onUnmounted(() => {
  if (videoRef.value) {
    videoRef.value.srcObject = null;
  }
});
</script>
