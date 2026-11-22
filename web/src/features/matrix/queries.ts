import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import type { AllocationExplain, Cluster, MatrixSnapshot, NewCluster } from "@/lib/types"

const MATRIX_REFRESH_MS = 10_000

export const queryKeys = {
  clusters: ["clusters"] as const,
  matrix: (clusterId: string) => ["clusters", clusterId, "matrix"] as const,
  explain: (clusterId: string, index: string, shard: number, primary: boolean) =>
    ["clusters", clusterId, "explain", index, shard, primary] as const,
}

export function useClusters() {
  return useQuery({
    queryKey: queryKeys.clusters,
    queryFn: () => api<Cluster[]>("/clusters"),
  })
}

export function useCreateCluster() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (cluster: NewCluster) =>
      api<Cluster>("/clusters", { method: "POST", body: JSON.stringify(cluster) }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.clusters }),
  })
}

export function useDeleteCluster() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (clusterId: string) => api<void>(`/clusters/${clusterId}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.clusters }),
  })
}

/** Polls the matrix of one cluster. Disabled while no cluster is selected. */
export function useMatrix(clusterId: string | null) {
  return useQuery({
    queryKey: queryKeys.matrix(clusterId ?? ""),
    queryFn: () => api<MatrixSnapshot>(`/clusters/${clusterId}/matrix`),
    enabled: clusterId !== null,
    refetchInterval: MATRIX_REFRESH_MS,
  })
}

export interface ShardSelection {
  index: string
  shard: number
  primary: boolean
}

/** Fetches the allocation explanation for an unassigned shard. */
export function useAllocationExplain(clusterId: string, selection: ShardSelection | null, enabled: boolean) {
  return useQuery({
    queryKey: selection
      ? queryKeys.explain(clusterId, selection.index, selection.shard, selection.primary)
      : ["clusters", clusterId, "explain", "none"],
    queryFn: () => {
      const params = new URLSearchParams({
        index: selection!.index,
        shard: String(selection!.shard),
        primary: String(selection!.primary),
      })
      return api<AllocationExplain>(`/clusters/${clusterId}/allocation-explain?${params}`)
    },
    enabled: enabled && selection !== null,
    retry: false,
  })
}
