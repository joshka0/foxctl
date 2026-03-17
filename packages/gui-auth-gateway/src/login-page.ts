import type { GatewayConfig } from "./config.js";

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

export function renderLoginPage(config: GatewayConfig, error?: string): string {
  const allowlistHint =
    config.allowedEmails.length > 0
      ? `<p class="hint">Access is limited to an approved email allowlist.</p>`
      : `<p class="hint">Request a magic link and we will sign you in without a password.</p>`;

  const errorBlock = error
    ? `<div class="error" role="alert">${escapeHtml(error)}</div>`
    : "";

  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Sign in to gui-agent</title>
    <style>
      :root {
        color-scheme: light;
        --bg: #f5efe2;
        --panel: rgba(255, 252, 245, 0.92);
        --text: #1d2a24;
        --muted: #5d6a64;
        --line: rgba(29, 42, 36, 0.14);
        --accent: #1f7a56;
        --accent-strong: #15563d;
        --error: #a12d2f;
      }
      * { box-sizing: border-box; }
      body {
        margin: 0;
        min-height: 100vh;
        display: grid;
        place-items: center;
        padding: 24px;
        font-family: "Iowan Old Style", "Palatino Linotype", "Book Antiqua", serif;
        background:
          radial-gradient(circle at top right, rgba(31, 122, 86, 0.16), transparent 28%),
          radial-gradient(circle at left center, rgba(146, 93, 46, 0.10), transparent 24%),
          linear-gradient(180deg, #f7f2e7 0%, var(--bg) 100%);
        color: var(--text);
      }
      .card {
        width: min(100%, 460px);
        border: 1px solid var(--line);
        background: var(--panel);
        backdrop-filter: blur(10px);
        border-radius: 24px;
        padding: 28px;
        box-shadow: 0 30px 80px rgba(38, 45, 40, 0.12);
      }
      .eyebrow {
        font-size: 12px;
        letter-spacing: 0.16em;
        text-transform: uppercase;
        color: var(--muted);
        margin: 0 0 10px;
      }
      h1 {
        margin: 0;
        font-size: clamp(2rem, 4vw, 2.75rem);
        line-height: 1;
      }
      p {
        color: var(--muted);
        margin: 12px 0 0;
        line-height: 1.5;
      }
      form {
        margin-top: 24px;
        display: grid;
        gap: 12px;
      }
      label {
        font-size: 13px;
        color: var(--muted);
      }
      input {
        width: 100%;
        padding: 14px 16px;
        border-radius: 14px;
        border: 1px solid var(--line);
        font: inherit;
        background: rgba(255,255,255,0.9);
      }
      button {
        appearance: none;
        border: 0;
        border-radius: 14px;
        padding: 14px 18px;
        font: inherit;
        font-weight: 700;
        color: white;
        background: linear-gradient(180deg, var(--accent) 0%, var(--accent-strong) 100%);
        cursor: pointer;
      }
      button[disabled] { opacity: 0.65; cursor: wait; }
      .hint {
        font-size: 14px;
      }
      .status {
        margin-top: 12px;
        font-size: 14px;
        color: var(--muted);
      }
      .error {
        margin-top: 18px;
        padding: 12px 14px;
        border-radius: 14px;
        background: rgba(161, 45, 47, 0.08);
        color: var(--error);
      }
    </style>
  </head>
  <body>
    <main class="card">
      <p class="eyebrow">Public Control Plane</p>
      <h1>Sign in to gui-agent</h1>
      ${allowlistHint}
      ${errorBlock}
      <form id="login-form">
        <div>
          <label for="email">Email address</label>
          <input id="email" name="email" type="email" autocomplete="email" required />
        </div>
        <button id="submit" type="submit">Send magic link</button>
      </form>
      <p class="status" id="status"></p>
    </main>
    <script>
      const form = document.getElementById("login-form");
      const submit = document.getElementById("submit");
      const status = document.getElementById("status");
      form.addEventListener("submit", async (event) => {
        event.preventDefault();
        submit.disabled = true;
        status.textContent = "Sending magic link...";
        const formData = new FormData(form);
        const email = formData.get("email");
        try {
          const response = await fetch("/api/auth/sign-in/magic-link", {
            method: "POST",
            headers: { "Content-Type": "application/json", "Accept": "application/json" },
            body: JSON.stringify({
              email,
              callbackURL: "/"
            })
          });
          if (!response.ok) {
            const text = await response.text();
            throw new Error(text || "Failed to send magic link");
          }
          status.textContent = "Magic link sent. Check your email to finish signing in.";
        } catch (error) {
          status.textContent = error instanceof Error ? error.message : "Failed to send magic link";
        } finally {
          submit.disabled = false;
        }
      });
    </script>
  </body>
</html>`;
}
