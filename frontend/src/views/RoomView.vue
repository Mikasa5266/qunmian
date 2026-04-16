<template>
  <section class="room-page" v-if="room">
    <header class="room-head">
      <div>
        <h1>{{ room.name }}</h1>
        <p>邀请码 {{ room.inviteCode }} · {{ room.participants.length }}/{{ room.maxParticipants }} 人</p>
      </div>
      <div class="room-actions">
        <button class="ghost-btn" @click="refreshRoom">刷新</button>
        <button class="secondary-btn" @click="backToHub">返回工作台</button>
      </div>
    </header>

    <div class="question-bubble">
      <div class="bubble-title">出题窗口</div>
      <p>{{ room.question || "群面尚未开始，满 3 人后点击开始群面。" }}</p>
      <button class="secondary-btn" v-if="room.started" @click="changeQuestion">换一题</button>
    </div>

    <div class="room-layout">
      <div class="video-column" :class="leftLayoutClass">
        <ParticipantTile
          v-for="(slot, idx) in leftSlots"
          :key="slot ? slot.userId : `empty-${idx}`"
          :participant="slot"
          :stream="streamFor(slot)"
          :isSelf="Boolean(slot && slot.userId === myUserId)"
        />
      </div>

      <aside class="chat-panel chat-center" :class="{ disconnected: !wsReady }">
        <h2>公屏</h2>
        <div class="chat-list" ref="chatListRef">
          <TransitionGroup name="chat-list" tag="div" class="chat-item-group">
            <div class="chat-item" v-for="message in messages" :key="message.id" :class="{ system: message.system }">
              <strong>{{ message.username }}</strong>
              <small v-if="message.time">{{ message.time }}</small>
              <span>{{ message.text }}</span>
            </div>
          </TransitionGroup>
        </div>
        <form class="chat-form" @submit.prevent="sendChat">
          <input v-model="chatInput" placeholder="请输入公屏消息" maxlength="200" />
          <button class="primary-btn" type="submit" :disabled="!wsReady">发送</button>
        </form>
      </aside>

      <div class="video-column" :class="rightLayoutClass">
        <ParticipantTile
          v-for="(slot, idx) in rightSlots"
          :key="slot ? slot.userId : `right-empty-${idx}`"
          :participant="slot"
          :stream="streamFor(slot)"
          :isSelf="Boolean(slot && slot.userId === myUserId)"
        />
      </div>
    </div>

    <footer class="control-bar">
      <button class="primary-btn" @click="toggleMic">
        {{ isMicMuted ? "开麦" : "闭麦" }}
      </button>
      <button class="primary-btn" @click="startInterview" :disabled="!canStartInterview">
        {{ room.started ? "群面进行中" : `开始群面（至少 ${room.minRequired} 人）` }}
      </button>
      <button class="danger-btn" @click="endInterview" :disabled="!canEndInterview">结束群面</button>
      <span class="info-text">视频为固定开启，不提供手动关闭。</span>
    </footer>

    <p class="error-text" v-if="errorText">{{ errorText }}</p>
  </section>

  <section class="room-page" v-else>
    <p>正在加载房间信息...</p>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import ParticipantTile from "../components/ParticipantTile.vue";
import { authState } from "../auth";
import {
  buildWsUrl,
  endRoom,
  getRoomState,
  getWebRTCConfig,
  nextQuestion,
  startRoom,
  type IceServer,
  type Participant,
  type RoomState
} from "../api";

interface IncomingWS {
  type: string;
  payload: unknown;
}

interface ChatItem {
  id: string;
  username: string;
  text: string;
  time: string;
  system: boolean;
}

interface ChatPayload {
  username: string;
  text: string;
  timestamp?: string;
}

interface SystemPayload {
  text: string;
  timestamp?: string;
}

type SignalType = "offer" | "answer" | "candidate";

interface SignalPayload {
  type: SignalType;
  sdp?: string;
  candidate?: RTCIceCandidateInit;
}

interface SignalMessagePayload {
  from: string;
  data: SignalPayload;
}

const route = useRoute();
const router = useRouter();
const roomId = String(route.params.roomId || "");

const room = ref<RoomState | null>(null);
const messages = ref<ChatItem[]>([]);
const chatInput = ref("");
const errorText = ref("");
const chatListRef = ref<HTMLDivElement | null>(null);

const localStream = ref<MediaStream | null>(null);
const remoteStreams = ref<Record<string, MediaStream>>({});
const peers = new Map<string, RTCPeerConnection>();

const iceServers = ref<IceServer[]>([{ urls: ["stun:stun.l.google.com:19302"] }]);
const isMicMuted = ref(false);
const wsReady = ref(false);
const wsConnectedOnce = ref(false);

let ws: WebSocket | null = null;
let destroyed = false;
let reconnectTimer: number | null = null;

const myUserId = computed(() => authState.user?.id || "");

const slots = computed<(Participant | null)[]>(() => {
  const participants = room.value?.participants || [];
  const fixed: (Participant | null)[] = [];
  for (let i = 0; i < 5; i += 1) {
    fixed.push(participants[i] || null);
  }
  return fixed;
});

const leftSlots = computed<(Participant | null)[]>(() => slots.value.slice(0, 2));
const rightSlots = computed<(Participant | null)[]>(() => slots.value.slice(2, 5));

function occupiedCount(items: (Participant | null)[]): number {
  return items.reduce((count, item) => (item ? count + 1 : count), 0);
}

const leftLayoutClass = computed(() => (occupiedCount(leftSlots.value) <= 1 ? "single-tile" : "double-tiles"));

const rightLayoutClass = computed(() => {
  const active = occupiedCount(rightSlots.value);
  if (active <= 1) {
    return "single-tile";
  }
  return active === 2 ? "double-tiles" : "triple-tiles";
});

const canStartInterview = computed(() => {
  if (!room.value) {
    return false;
  }
  return !room.value.started && room.value.participants.length >= room.value.minRequired;
});

const canEndInterview = computed(() => Boolean(room.value?.started));

function backToHub(): void {
  void router.push("/hub");
}

function formatTime(timestamp?: string): string {
  if (!timestamp) {
    return "";
  }
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  return date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function appendChat(username: string, text: string, options: { timestamp?: string; system?: boolean } = {}): void {
  messages.value.push({
    id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
    username,
    text,
    time: formatTime(options.timestamp),
    system: Boolean(options.system)
  });

  if (messages.value.length > 200) {
    messages.value.shift();
  }

  void nextTick(() => {
    if (chatListRef.value) {
      chatListRef.value.scrollTop = chatListRef.value.scrollHeight;
    }
  });
}

function sendWS(type: string, payload: unknown): void {
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    return;
  }
  ws.send(JSON.stringify({ type, payload }));
}

function sendChat(): void {
  const text = chatInput.value.trim();
  if (!text) {
    return;
  }
  if (!wsReady.value) {
    errorText.value = "公屏连接尚未建立，请稍后重试";
    return;
  }
  sendWS("chat", { text });
  chatInput.value = "";
}

function syncMicTrack(): void {
  if (!localStream.value) {
    return;
  }
  for (const track of localStream.value.getAudioTracks()) {
    track.enabled = !isMicMuted.value;
  }
  sendWS("mute", { muted: isMicMuted.value });
}

function toggleMic(): void {
  isMicMuted.value = !isMicMuted.value;
  syncMicTrack();
}

async function runRoomAction(action: () => Promise<{ room: RoomState }>, fallbackError: string): Promise<boolean> {
  try {
    const data = await action();
    room.value = data.room;
    errorText.value = "";
    return true;
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : fallbackError;
    return false;
  }
}

async function refreshRoom(): Promise<void> {
  const ok = await runRoomAction(() => getRoomState(roomId), "刷新房间失败");
  if (ok) {
    ensurePeers();
  }
}

async function startInterview(): Promise<void> {
  await runRoomAction(() => startRoom(roomId), "开始群面失败");
}

async function endInterview(): Promise<void> {
  const ok = await runRoomAction(() => endRoom(roomId), "结束群面失败");
  if (ok) {
    appendChat("系统", "群面已结束，可继续讨论或返回工作台", { system: true });
  }
}

async function changeQuestion(): Promise<void> {
  await runRoomAction(() => nextQuestion(roomId), "切题失败");
}

function streamFor(slot: Participant | null): MediaStream | null {
  if (!slot) {
    return null;
  }
  if (slot.userId === myUserId.value) {
    return localStream.value;
  }
  return remoteStreams.value[slot.userId] || null;
}

function shouldInitiate(peerId: string): boolean {
  return myUserId.value < peerId;
}

async function createOffer(peerId: string): Promise<void> {
  const pc = peers.get(peerId);
  if (!pc || pc.signalingState !== "stable") {
    return;
  }
  const offer = await pc.createOffer();
  await pc.setLocalDescription(offer);
  sendWS("signal", {
    to: peerId,
    data: { type: "offer", sdp: offer.sdp }
  });
}

function createPeer(peerId: string): RTCPeerConnection {
  const existing = peers.get(peerId);
  if (existing) {
    return existing;
  }

  const pc = new RTCPeerConnection({ iceServers: iceServers.value as RTCIceServer[] });

  if (localStream.value) {
    for (const track of localStream.value.getTracks()) {
      pc.addTrack(track, localStream.value);
    }
  }

  pc.onicecandidate = (event) => {
    if (!event.candidate) {
      return;
    }
    sendWS("signal", {
      to: peerId,
      data: {
        type: "candidate",
        candidate: event.candidate.toJSON()
      }
    });
  };

  pc.ontrack = (event) => {
    const stream = event.streams[0];
    if (stream) {
      remoteStreams.value[peerId] = stream;
    }
  };

  pc.onconnectionstatechange = () => {
    if (pc.connectionState === "failed" || pc.connectionState === "closed") {
      delete remoteStreams.value[peerId];
    }
  };

  peers.set(peerId, pc);

  if (shouldInitiate(peerId)) {
    void createOffer(peerId);
  }

  return pc;
}

function attachLocalTracksToPeers(): void {
  if (!localStream.value) {
    return;
  }

  const tracks = localStream.value.getTracks();
  for (const pc of peers.values()) {
    const senderTrackIds = new Set(
      pc
        .getSenders()
        .map((sender) => sender.track?.id)
        .filter((id): id is string => Boolean(id))
    );
    for (const track of tracks) {
      if (!senderTrackIds.has(track.id)) {
        pc.addTrack(track, localStream.value);
      }
    }
  }
}

async function handleSignal(from: string, signal: SignalPayload): Promise<void> {
  const pc = createPeer(from);

  if (signal.type === "offer") {
    if (pc.signalingState !== "stable") {
      await pc.setLocalDescription({ type: "rollback" } as RTCLocalSessionDescriptionInit).catch(() => undefined);
    }
    await pc.setRemoteDescription({ type: "offer", sdp: signal.sdp || "" });
    const answer = await pc.createAnswer();
    await pc.setLocalDescription(answer);
    sendWS("signal", {
      to: from,
      data: { type: "answer", sdp: answer.sdp }
    });
    return;
  }

  if (signal.type === "answer") {
    if (pc.signalingState === "have-local-offer") {
      await pc.setRemoteDescription({ type: "answer", sdp: signal.sdp || "" });
    }
    return;
  }

  if (signal.type === "candidate" && signal.candidate) {
    await pc.addIceCandidate(signal.candidate).catch(() => undefined);
  }
}

function ensurePeers(): void {
  if (!room.value || !myUserId.value) {
    return;
  }

  const onlineOthers = room.value.participants
    .filter((p) => p.userId !== myUserId.value && p.online)
    .map((p) => p.userId);

  for (const peerId of onlineOthers) {
    createPeer(peerId);
  }

  for (const [peerId, pc] of peers.entries()) {
    if (!onlineOthers.includes(peerId)) {
      pc.close();
      peers.delete(peerId);
      delete remoteStreams.value[peerId];
    }
  }
}

function connectWS(): void {
  if (!authState.token) {
    return;
  }

  ws = new WebSocket(buildWsUrl(roomId, authState.token));

  ws.onopen = () => {
    wsReady.value = true;
    syncMicTrack();
    ensurePeers();
    if (!wsConnectedOnce.value) {
      appendChat("系统", "公屏连接成功", { system: true });
      wsConnectedOnce.value = true;
    }
  };

  ws.onmessage = (event) => {
    const incoming = JSON.parse(event.data) as IncomingWS;

    if (incoming.type === "room_state") {
      room.value = incoming.payload as RoomState;
      ensurePeers();
      return;
    }

    if (incoming.type === "chat") {
      const payload = incoming.payload as ChatPayload;
      appendChat(payload.username, payload.text, { timestamp: payload.timestamp });
      return;
    }

    if (incoming.type === "system") {
      const payload = incoming.payload as SystemPayload;
      appendChat("系统", payload.text, { timestamp: payload.timestamp, system: true });
      return;
    }

    if (incoming.type === "signal") {
      const payload = incoming.payload as SignalMessagePayload;
      void handleSignal(String(payload.from), payload.data);
    }
  };

  ws.onclose = () => {
    wsReady.value = false;
    if (!destroyed) {
      reconnectTimer = window.setTimeout(() => {
        connectWS();
      }, 2000);
    }
  };
}

async function initMedia(): Promise<void> {
  const mediaDevices = navigator.mediaDevices;
  if (!mediaDevices || typeof mediaDevices.getUserMedia !== "function") {
    throw new Error("当前环境不支持媒体设备访问，请用 HTTPS 或 localhost 打开");
  }

  localStream.value = await mediaDevices.getUserMedia({
    video: true,
    audio: true
  });
  syncMicTrack();
  attachLocalTracksToPeers();
}

async function initRoom(): Promise<void> {
  try {
    const [roomData, rtcConfig] = await Promise.all([getRoomState(roomId), getWebRTCConfig()]);
    room.value = roomData.room;
    iceServers.value = rtcConfig.iceServers;
    connectWS();
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : "初始化房间失败";
    return;
  }

  try {
    await initMedia();
  } catch (error) {
    const message = error instanceof Error ? error.message : "无法打开摄像头或麦克风";
    errorText.value = message;
    appendChat("系统", `音视频未接通：${message}`, { system: true });
  }
}

onMounted(() => {
  void initRoom();
});

onUnmounted(() => {
  destroyed = true;
  wsReady.value = false;
  if (reconnectTimer !== null) {
    window.clearTimeout(reconnectTimer);
  }
  if (ws) {
    ws.close();
  }

  for (const pc of peers.values()) {
    pc.close();
  }
  peers.clear();

  if (localStream.value) {
    for (const track of localStream.value.getTracks()) {
      track.stop();
    }
  }
});
</script>
