import { reactive } from "vue";

export interface AuthUser {
  id: string;
  username: string;
}

const savedToken = localStorage.getItem("token") || "";
const savedUser = localStorage.getItem("user");

export const authState = reactive<{ token: string; user: AuthUser | null }>({
  token: savedToken,
  user: savedUser ? (JSON.parse(savedUser) as AuthUser) : null,
});

export function setAuth(token: string, user: AuthUser): void {
  authState.token = token;
  authState.user = user;
  localStorage.setItem("token", token);
  localStorage.setItem("user", JSON.stringify(user));
}

export function clearAuth(): void {
  authState.token = "";
  authState.user = null;
  localStorage.removeItem("token");
  localStorage.removeItem("user");
}

export function isAuthed(): boolean {
  return Boolean(authState.token && authState.user);
}
