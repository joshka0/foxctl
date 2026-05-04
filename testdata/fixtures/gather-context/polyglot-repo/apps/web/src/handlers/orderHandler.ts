import { createOrder } from "../services/orderService";

export function createOrderHandler() {
  return async function handleCreateOrder(request: { body: unknown }) {
    return createOrder(request.body);
  };
}
