import { CheckOutlined, CloseOutlined, SearchOutlined } from '@ant-design/icons'
import { Alert, Button, Form, Input, Modal, Select, Space, Typography, message } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { pauseRequestAPI } from '../api'
import { EntityTable } from '../components/common/EntityTable'
import { PauseRequestStatusBadge } from '../components/common/PauseRequestStatusBadge'
import { useAuth } from '../hooks/useAuth'
import { usePagination } from '../hooks/usePagination'
import { usePauseRequestStore } from '../stores/pauseRequestStore'
import type { PauseRequest, PauseRequestStatus, PauseReviewAction } from '../types/domain'
import { formatDateTime } from '../utils/format'

export function PauseRequestsPage() {
  const { data, loading, load } = usePauseRequestStore()
  const pagination = usePagination()
  const { can } = useAuth()
  const navigate = useNavigate()
  const [status, setStatus] = useState<PauseRequestStatus | undefined>()
  const [reviewTarget, setReviewTarget] = useState<{ request: PauseRequest; action: PauseReviewAction } | null>(null)
  const [saving, setSaving] = useState(false)
  const [form] = Form.useForm<{ comment: string }>()
  const refresh = () => load({ page: pagination.page, pageSize: pagination.pageSize, status })
  useEffect(() => { void refresh() }, [pagination.page, pagination.pageSize, status])
  const review = async () => {
    if (!reviewTarget) return
    const values = await form.validateFields()
    setSaving(true)
    try {
      await pauseRequestAPI.review(reviewTarget.request.id, reviewTarget.action, values.comment || '')
      message.success(reviewTarget.action === 'approve' ? '已同意暂停，批次进入暂停' : '已拒绝暂停申请，批次保持运行')
      setReviewTarget(null)
      form.resetFields()
      await refresh()
    } finally { setSaving(false) }
  }
  const columns: ColumnsType<PauseRequest> = [
    { title: '批次', render: (_, row) => <Button className="table-link" type="link" onClick={() => navigate(`/batches/${row.productionBatchId}`)}>{row.productionBatch?.batchNo || row.productionBatchId}</Button>, fixed: 'left' },
    { title: '申请原因', dataIndex: 'reason', ellipsis: true },
    { title: '申请人', dataIndex: 'applicantName' },
    { title: '申请时间', dataIndex: 'createdAt', render: formatDateTime },
    { title: '状态', dataIndex: 'status', render: (value) => <PauseRequestStatusBadge status={value} /> },
    { title: '审批人', dataIndex: 'reviewerName', render: (value) => value || '-' },
    { title: '审批结论', dataIndex: 'reviewComment', ellipsis: true, render: (value) => value || '-' },
    { title: '审批时间', dataIndex: 'reviewedAt', render: (value) => value ? formatDateTime(value) : '-' },
    {
      title: '操作', fixed: 'right', render: (_, row) => row.status === 'pending' && can('release:write') && (
        <Space>
          <Button size="small" type="primary" icon={<CheckOutlined />} onClick={() => setReviewTarget({ request: row, action: 'approve' })}>同意</Button>
          <Button size="small" danger icon={<CloseOutlined />} onClick={() => setReviewTarget({ request: row, action: 'reject' })}>拒绝</Button>
        </Space>
      ),
    },
  ]
  return (
    <div className="page-stack">
      <header className="page-header"><div><Typography.Title level={2}>暂停复核</Typography.Title><Typography.Text type="secondary">审批产线提交的批次暂停申请，同意后批次进入暂停，拒绝则保持运行</Typography.Text></div></header>
      <div className="table-toolbar"><Select allowClear placeholder="全部状态" value={status} onChange={setStatus} options={[{ value: 'pending', label: '待审批' }, { value: 'approved', label: '已同意' }, { value: 'rejected', label: '已拒绝' }]} /><Button icon={<SearchOutlined />} onClick={() => void refresh()}>查询</Button></div>
      <EntityTable columns={columns} dataSource={data.items} loading={loading} emptyTitle="暂无暂停申请" pagination={{ current: pagination.page, pageSize: pagination.pageSize, total: data.total, onChange: pagination.update, showSizeChanger: true }} />
      <Modal
        title={reviewTarget?.action === 'approve' ? `同意暂停 · ${reviewTarget?.request.productionBatch?.batchNo || ''}` : `拒绝暂停 · ${reviewTarget?.request.productionBatch?.batchNo || ''}`}
        open={Boolean(reviewTarget)} confirmLoading={saving} onOk={() => void review()} onCancel={() => setReviewTarget(null)} okText="确认" cancelText="取消"
      >
        {reviewTarget && (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Alert type="info" showIcon message={`申请原因：${reviewTarget.request.reason}`} description={`申请人：${reviewTarget.request.applicantName} · ${formatDateTime(reviewTarget.request.createdAt)}`} />
            {reviewTarget.action === 'approve'
              ? <Alert type="warning" showIcon message="同意后批次立即进入暂停状态，需恢复后才能继续生产。" />
              : <Alert type="warning" showIcon message="拒绝后批次保持运行，必须填写审批结论。" />}
            <Form form={form} layout="vertical">
              <Form.Item
                label="审批结论" name="comment"
                rules={reviewTarget.action === 'reject' ? [{ required: true, min: 3, message: '拒绝时必须填写至少 3 个字的审批结论' }] : []}
              >
                <Input.TextArea rows={3} maxLength={500} showCount placeholder={reviewTarget.action === 'approve' ? '可填写补充说明（选填）' : '请填写拒绝原因'} />
              </Form.Item>
            </Form>
          </Space>
        )}
      </Modal>
    </div>
  )
}
