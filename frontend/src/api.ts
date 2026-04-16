import { authState, clearAuth, type AuthUser } from "./auth";

const API_BASE = import.meta.env.VITE_API_BASE || "";

export interface Participant {
  userId: string;
  username: string;
  muted: boolean;
  online: boolean;
}

export interface RoomState {
  roomId: string;
  name: string;
  inviteCode: string;
  started: boolean;
  question: string;
  maxParticipants: number;
  minRequired: number;
  participants: Participant[];
  updatedAt: string;
}

export interface IceServer {
  urls: string[];
  username?: string;
  credential?: string;
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers || {});
  headers.set("Content-Type", "application/json");
  if (authState.token) {
    headers.set("Authorization", `Bearer ${authState.token}`);
  }

  const resp = await fetch(`${API_BASE}${path}`, { ...options, headers });
  const data = (await resp.json().catch(() => ({}))) as { error?: string } & T;
  if (!resp.ok) {
    if (resp.status === 401) {
      clearAuth();
    }
    throw new Error(data.error || "请求失败");
  }
  return data;
}

export async function register(
  username: string,
  password: string,
): Promise<{ token: string; user: AuthUser }> {
  return request("/api/auth/register", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
}

export async function login(
  username: string,
  password: string,
): Promise<{ token: string; user: AuthUser }> {
  return request("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
}

export async function createInvite(roomName: string): Promise<{
  roomId: string;
  roomName: string;
  inviteCode: string;
  inviteLink: string;
}> {
  return request("/api/invites", {
    method: "POST",
    body: JSON.stringify({ roomName }),
  });
}

export async function getInvite(
  code: string,
): Promise<{ inviteCode: string; room: RoomState }> {
  return request(`/api/invites/${encodeURIComponent(code)}`);
}

export async function acceptInvite(code: string): Promise<{ room: RoomState }> {
  return request(`/api/invites/${encodeURIComponent(code)}/accept`, {
    method: "POST",
  });
}

export async function myRooms(): Promise<{ rooms: RoomState[] }> {
  return request("/api/rooms/mine");
}

export async function getRoomState(
  roomId: string,
): Promise<{ room: RoomState }> {
  return request(`/api/rooms/${encodeURIComponent(roomId)}/state`);
}

export async function startRoom(roomId: string): Promise<{ room: RoomState }> {
  return request(`/api/rooms/${encodeURIComponent(roomId)}/start`, {
    method: "POST",
  });
}

export async function endRoom(roomId: string): Promise<{ room: RoomState }> {
  return request(`/api/rooms/${encodeURIComponent(roomId)}/end`, {
    method: "POST",
  });
}

export async function nextQuestion(
  roomId: string,
): Promise<{ room: RoomState }> {
  return request(`/api/rooms/${encodeURIComponent(roomId)}/question/next`, {
    method: "POST",
  });
}

export async function getWebRTCConfig(): Promise<{ iceServers: IceServer[] }> {
  return request("/api/config/webrtc");
}

export function buildWsUrl(roomId: string, token: string): string {
  const customWsBase = import.meta.env.VITE_WS_BASE;
  if (customWsBase) {
    return `${customWsBase}/ws?roomId=${encodeURIComponent(roomId)}&token=${encodeURIComponent(token)}`;
  }

  if (API_BASE.startsWith("http://") || API_BASE.startsWith("https://")) {
    const wsBase = API_BASE.replace("http://", "ws://").replace(
      "https://",
      "wss://",
    );
    return `${wsBase}/ws?roomId=${encodeURIComponent(roomId)}&token=${encodeURIComponent(token)}`;
  }

  const protocol = window.location.protocol === "https:" ? "wss" : "ws";
  return `${protocol}://${window.location.host}/ws?roomId=${encodeURIComponent(roomId)}&token=${encodeURIComponent(token)}`;
}
