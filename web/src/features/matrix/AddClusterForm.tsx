import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import type { Environment, NewCluster } from "@/lib/types"
import { useCreateCluster } from "./queries"

const EMPTY: NewCluster = {
  name: "",
  url: "",
  username: "",
  password: "",
  env: "dev",
  insecure_skip_verify: false,
}

interface AddClusterFormProps {
  onCreated: (clusterId: string) => void
  onCancel?: () => void
}

export function AddClusterForm({ onCreated, onCancel }: AddClusterFormProps) {
  const [form, setForm] = useState<NewCluster>(EMPTY)
  const create = useCreateCluster()

  const update = <K extends keyof NewCluster>(key: K, value: NewCluster[K]) =>
    setForm((current) => ({ ...current, [key]: value }))

  const submit = (event: FormEvent) => {
    event.preventDefault()
    create.mutate(form, {
      onSuccess: (cluster) => {
        setForm(EMPTY)
        onCreated(cluster.id)
      },
    })
  }

  return (
    <form onSubmit={submit} className="grid max-w-xl gap-3 rounded-md border border-border bg-card p-4">
      <h2 className="text-base font-semibold">Connect a cluster</h2>

      <label className="grid gap-1 text-sm">
        Name
        <Input required value={form.name} onChange={(e) => update("name", e.target.value)} placeholder="prod-logs" />
      </label>
      <label className="grid gap-1 text-sm">
        URL
        <Input
          required
          type="url"
          value={form.url}
          onChange={(e) => update("url", e.target.value)}
          placeholder="https://es.example.com:9200"
        />
      </label>
      <div className="grid grid-cols-2 gap-3">
        <label className="grid gap-1 text-sm">
          Username (optional)
          <Input autoComplete="off" value={form.username} onChange={(e) => update("username", e.target.value)} />
        </label>
        <label className="grid gap-1 text-sm">
          Password (optional)
          <Input
            type="password"
            autoComplete="new-password"
            value={form.password}
            onChange={(e) => update("password", e.target.value)}
          />
        </label>
      </div>
      <label className="grid gap-1 text-sm">
        Environment
        <select
          className="h-9 rounded-md border border-border bg-transparent px-2 text-sm"
          value={form.env}
          onChange={(e) => update("env", e.target.value as Environment)}
        >
          <option value="dev">dev</option>
          <option value="staging">staging</option>
          <option value="prod">prod</option>
        </select>
      </label>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={form.insecure_skip_verify}
          onChange={(e) => update("insecure_skip_verify", e.target.checked)}
        />
        Skip TLS certificate verification (not recommended)
      </label>

      {create.isError && <p className="text-sm text-red-400">{create.error.message}</p>}

      <div className="flex gap-2">
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? "Connecting..." : "Connect"}
        </Button>
        {onCancel && (
          <Button type="button" variant="outline" onClick={onCancel}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  )
}
