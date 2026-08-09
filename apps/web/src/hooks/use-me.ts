import { useQuery, type UseQueryOptions } from "@tanstack/react-query"
import { api, isUnauthorizedError } from "@/lib/api"
import type { User } from "@/lib/api"

type MeResponse = { user: User }

export function useMe(
  options?: Omit<UseQueryOptions<MeResponse, Error, MeResponse, ["me"]>, "queryKey" | "queryFn">,
) {
  return useQuery({
    queryKey: ["me"],
    queryFn: api.me,
    retry: (failureCount, error) => !isUnauthorizedError(error) && failureCount < 1,
    ...options,
  })
}
