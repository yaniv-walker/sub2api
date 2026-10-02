import { apiClient } from '../client'

export interface UpstreamRetryGuardrailsSettings {
  enabled: boolean
}

export async function getUpstreamRetryGuardrailsSettings(): Promise<UpstreamRetryGuardrailsSettings> {
  const { data } = await apiClient.get<UpstreamRetryGuardrailsSettings>('/admin/settings/upstream-retry-guardrails')
  return data
}

export async function updateUpstreamRetryGuardrailsSettings(enabled: boolean): Promise<UpstreamRetryGuardrailsSettings> {
  const { data } = await apiClient.put<UpstreamRetryGuardrailsSettings>('/admin/settings/upstream-retry-guardrails', { enabled })
  return data
}
