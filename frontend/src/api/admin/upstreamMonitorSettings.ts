import { apiClient } from '../client'

export interface UpstreamMonitorSettings {
  enabled: boolean
}

export async function getUpstreamMonitorSettings(): Promise<UpstreamMonitorSettings> {
  const { data } = await apiClient.get<UpstreamMonitorSettings>('/admin/settings/upstream-monitor')
  return data
}

export async function updateUpstreamMonitorSettings(enabled: boolean): Promise<UpstreamMonitorSettings> {
  const { data } = await apiClient.put<UpstreamMonitorSettings>('/admin/settings/upstream-monitor', { enabled })
  return data
}
