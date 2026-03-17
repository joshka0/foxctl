import nodemailer from "nodemailer";

import type { GatewayConfig } from "./config.js";

export async function sendMagicLinkEmail(
  config: GatewayConfig,
  email: string,
  url: string,
): Promise<void> {
  if (config.magicLink.logOnly) {
    console.info(`[gui-auth-gateway] magic link for ${email}: ${url}`);
    return;
  }

  if (!config.smtp) {
    throw new Error("SMTP is not configured and log-only magic links are disabled");
  }

  const transporter = nodemailer.createTransport({
    host: config.smtp.host,
    port: config.smtp.port,
    secure: config.smtp.secure,
    auth:
      config.smtp.user || config.smtp.pass
        ? {
            user: config.smtp.user,
            pass: config.smtp.pass,
          }
        : undefined,
  });

  await transporter.sendMail({
    from: config.smtp.from,
    to: email,
    replyTo: config.smtp.replyTo,
    subject: "Your agentctl gui-agent sign-in link",
    text: `Sign in to gui-agent by opening this link:\n\n${url}\n\nIf you did not request this link, you can ignore this email.`,
    html: `<p>Sign in to <strong>gui-agent</strong> by opening this link:</p><p><a href="${url}">${url}</a></p><p>If you did not request this link, you can ignore this email.</p>`,
  });
}
