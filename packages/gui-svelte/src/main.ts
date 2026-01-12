import "./app.css";
import Root from "./Root.svelte";

// Initialize theme from localStorage or system preference
function initTheme() {
  const stored = localStorage.getItem("theme");
  if (stored === "dark" || (!stored && window.matchMedia("(prefers-color-scheme: dark)").matches)) {
    document.documentElement.classList.add("dark");
  } else {
    document.documentElement.classList.remove("dark");
  }
}

initTheme();

const app = new Root({
  target: document.getElementById("app")!,
});

export default app;
