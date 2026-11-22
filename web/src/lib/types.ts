// Types mirroring the Searchboard HTTP API. See docs/api.md.

export type Environment = "prod" | "staging" | "dev"
export type HealthStatus = "green" | "yellow" | "red"
export type ShardState = "STARTED" | "INITIALIZING" | "RELOCATING" | "UNASSIGNED"

export interface Cluster {
  id: string
  name: string
  url: string
  username: string
  env: Environment
  insecure_skip_verify: boolean
  created_at: string
}

export interface NewCluster {
  name: string
  url: string
  username: string
  password: string
  env: Environment
  insecure_skip_verify: boolean
}

export interface ClusterHealth {
  cluster_name: string
  status: HealthStatus
  number_of_nodes: number
  number_of_data_nodes: number
  active_primary_shards: number
  active_shards: number
  relocating_shards: number
  initializing_shards: number
  unassigned_shards: number
}

export interface ClusterNode {
  id: string
  name: string
  ip: string
  roles: string
  is_master: boolean
  heap_pct?: number
  cpu_pct?: number
  disk_pct?: number
}

export interface ShardRef {
  shard: number
  primary: boolean
  state: ShardState
  docs?: number
  store_bytes?: number
  relocating_to?: string
  unassigned_reason?: string
}

export interface IndexRow {
  name: string
  health: HealthStatus
  cells: Record<string, ShardRef[] | undefined>
  unassigned: ShardRef[]
}

export interface MatrixSnapshot {
  cluster: Cluster
  version: string
  health: ClusterHealth
  nodes: ClusterNode[]
  indices: IndexRow[]
  generated_at: string
}

export interface AllocationDecider {
  name: string
  decision: string
  explanation: string
}

export interface AllocationNodeDecision {
  node_name: string
  decision: string
  deciders?: AllocationDecider[]
}

export interface AllocationExplain {
  index: string
  shard: number
  primary: boolean
  current_state: string
  unassigned_reason?: string
  can_allocate?: string
  explanation?: string
  node_decisions?: AllocationNodeDecision[]
}
