import { createOrderHandler } from "./handlers/orderHandler";

export function registerRoutes(router: { post(path: string, handler: unknown): void }) {
  router.post("/orders", createOrderHandler());
}
