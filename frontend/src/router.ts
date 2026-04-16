import { createRouter, createWebHistory } from "vue-router";
import { isAuthed } from "./auth";
import LoginView from "./views/LoginView.vue";
import HubView from "./views/HubView.vue";
import RoomView from "./views/RoomView.vue";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: () => (isAuthed() ? "/hub" : "/login") },
    { path: "/login", component: LoginView },
    { path: "/hub", component: HubView, meta: { requiresAuth: true } },
    {
      path: "/room/:roomId",
      component: RoomView,
      meta: { requiresAuth: true },
    },
  ],
});

router.beforeEach((to) => {
  if (to.meta.requiresAuth && !isAuthed()) {
    return "/login";
  }
  if (to.path === "/login" && isAuthed()) {
    return "/hub";
  }
  return true;
});
