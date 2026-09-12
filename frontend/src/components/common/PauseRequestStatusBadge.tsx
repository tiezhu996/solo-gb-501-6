import { Tag } from 'antd'
import type { PauseRequestStatus } from '../../types/domain'

const config: Record<PauseRequestStatus, { label: string; color: string }> = {
  pending: { label: '待审批', color: 'processing' },
  approved: { label: '已同意', color: 'success' },
  rejected: { label: '已拒绝', color: 'default' },
}

export function PauseRequestStatusBadge({ status }: { status: PauseRequestStatus }) {
  const item = config[status]
  return <Tag color={item.color}>{item.label}</Tag>
}
