import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getAuthSession,
  signOutAuthSession,
  type AuthSessionResponse,
} from "@/api/client";

const AUTH_SESSION_QUERY_KEY = ["auth-session"] as const;

export function useAuthSession() {
  return useQuery<AuthSessionResponse | null>({
    queryKey: AUTH_SESSION_QUERY_KEY,
    queryFn: getAuthSession,
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    refetchInterval: 60_000,
  });
}

export function useAuthSignOut() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: signOutAuthSession,
    onSuccess: async () => {
      queryClient.setQueryData(AUTH_SESSION_QUERY_KEY, null);
      await queryClient.invalidateQueries({ queryKey: AUTH_SESSION_QUERY_KEY });
      window.location.assign("/login");
    },
  });
}
