import { create } from "zustand";

interface RouterState {
  path: string;
  navigate: (path: string) => void;
  // redirect replaces the current history entry, so Back does not bounce.
  redirect: (path: string) => void;
}

export const useRouter = create<RouterState>((set) => ({
  path: window.location.pathname === "/" ? "/projects" : window.location.pathname,
  navigate: (path) => {
    window.history.pushState({}, "", path);
    set({ path });
  },
  redirect: (path) => {
    window.history.replaceState({}, "", path);
    set({ path });
  },
}));

window.addEventListener("popstate", () => {
  useRouter.setState({ path: window.location.pathname === "/" ? "/projects" : window.location.pathname });
});

export function routeParts(path: string) {
  return path.split("/").filter(Boolean);
}
