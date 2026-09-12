import { useCallback, useState } from 'react'
import { pauseRequestAPI, type PageParams } from '../api'
import type { PageResult, PauseRequest, PauseRequestStatus } from '../types/domain'

export function usePauseRequestStore() {
  const [data, setData] = useState<PageResult<PauseRequest>>({ items: [], total: 0, page: 1, pageSize: 10 })
  const [loading, setLoading] = useState(false)
  const load = useCallback(async (params: PageParams & { status?: PauseRequestStatus; batchId?: number } = {}) => {
    setLoading(true)
    try { setData(await pauseRequestAPI.list(params)) } finally { setLoading(false) }
  }, [])
  return { data, loading, load }
}
