import { ArrowLeftOutlined, RollbackOutlined } from '@ant-design/icons'
import { Alert, Button, Descriptions, Divider, Popconfirm, Space, Spin, Typography, message } from 'antd'
import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { batchAPI, pauseRequestAPI, releaseAPI } from '../api'
import { BatchStatusBadge } from '../components/common/BatchStatusBadge'
import { DecisionPanel } from '../components/common/DecisionPanel'
import { EntityTable } from '../components/common/EntityTable'
import { PauseRequestStatusBadge } from '../components/common/PauseRequestStatusBadge'
import { StatusBadge } from '../components/common/StatusBadge'
import { useAuth } from '../hooks/useAuth'
import type { DecisionType, InspectionSample, PauseRequest, ProductionBatch } from '../types/domain'
import { formatDateTime, formatNumber } from '../utils/format'

export function BatchDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { can, user } = useAuth()
  const [batch, setBatch] = useState<ProductionBatch | null>(null)
  const [pauseRequests, setPauseRequests] = useState<PauseRequest[]>([])
  const [loading, setLoading] = useState(true)
  const [withdrawing, setWithdrawing] = useState(false)
  const load = async () => {
    setLoading(true)
    try {
      const [batchData, pauseData] = await Promise.all([
        batchAPI.get(Number(id)),
        pauseRequestAPI.list({ page: 1, pageSize: 100, batchId: Number(id) }),
      ])
      setBatch(batchData)
      setPauseRequests(pauseData.items)
    } finally { setLoading(false) }
  }
  useEffect(() => { void load() }, [id])
  if (loading || !batch) return <Spin fullscreen />
  const pendingPause = pauseRequests.find((request) => request.status === 'pending')
  const decide = async (decision: DecisionType, reason: string) => { await releaseAPI.decide(batch.id, decision, reason); message.success('审批决定已提交'); await load() }
  const withdraw = async (request: PauseRequest) => {
    setWithdrawing(true)
    try {
      await pauseRequestAPI.withdraw(request.id)
      message.success('暂停申请已撤销，批次保持运行')
      await load()
    } finally { setWithdrawing(false) }
  }
  return (
    <div className="page-stack">
      <header className="page-header"><div><Button type="text" icon={<ArrowLeftOutlined />} onClick={() => navigate('/batches')}>返回批次队列</Button><Typography.Title level={2}>{batch.batchNo}</Typography.Title></div><BatchStatusBadge status={batch.status} /></header>
      {pendingPause && <Alert type="warning" showIcon message={`存在待审批的暂停申请（${pendingPause.applicantName}：${pendingPause.reason}），审批期间不能新增检验或提交放行决定`} />}
      <section className="detail-section"><Typography.Title level={4}>批次信息</Typography.Title><Descriptions column={{ xs: 1, sm: 2, lg: 3 }} bordered size="small"><Descriptions.Item label="规格">{batch.specification}</Descriptions.Item><Descriptions.Item label="责任班组">{batch.responsibleTeam}</Descriptions.Item><Descriptions.Item label="包装产线">{batch.packagingLine?.name || batch.packagingLineId}</Descriptions.Item><Descriptions.Item label="生产数量">{formatNumber(batch.producedQuantity)} / {formatNumber(batch.plannedQuantity)}</Descriptions.Item><Descriptions.Item label="开始时间">{formatDateTime(batch.startedAt)}</Descriptions.Item><Descriptions.Item label="完成时间">{formatDateTime(batch.completedAt)}</Descriptions.Item>{batch.holdReason && <Descriptions.Item label="暂停/处置原因" span={3}>{batch.holdReason}</Descriptions.Item>}</Descriptions></section>
      <section className="detail-section"><Typography.Title level={4}>检验明细</Typography.Title><EntityTable<InspectionSample> size="small" pagination={false} dataSource={batch.inspections || []} columns={[{ title: '样本', dataIndex: 'sampleCode' }, { title: '抽样位置', dataIndex: 'samplingPosition' }, { title: '检验项', dataIndex: 'inspectionItem' }, { title: '结果', dataIndex: 'result', render: (value) => <StatusBadge value={value} /> }, { title: '测量值', dataIndex: 'measuredValue' }, { title: '接受范围', dataIndex: 'acceptanceRange' }, { title: '复测', dataIndex: 'retestStatus', render: (value) => <StatusBadge value={value} /> }]} /></section>
      <section className="detail-section"><Typography.Title level={4}>暂停申请记录</Typography.Title><EntityTable<PauseRequest> size="small" pagination={false} dataSource={pauseRequests} emptyTitle="暂无暂停申请" columns={[{ title: '申请原因', dataIndex: 'reason', ellipsis: true }, { title: '申请人', dataIndex: 'applicantName' }, { title: '申请时间', dataIndex: 'createdAt', render: formatDateTime }, { title: '状态', dataIndex: 'status', render: (value) => <PauseRequestStatusBadge status={value} /> }, { title: '审批人', dataIndex: 'reviewerName', render: (value) => value || '-' }, { title: '审批结论', dataIndex: 'reviewComment', ellipsis: true, render: (value) => value || '-' }, { title: '审批时间', dataIndex: 'reviewedAt', render: (value) => value ? formatDateTime(value) : '-' }, { title: '操作', render: (_, row) => row.status === 'pending' && row.applicantId === user?.id && <Popconfirm title="撤销暂停申请" description="撤销后批次保持运行，确定撤销？" okText="确定" cancelText="取消" onConfirm={() => void withdraw(row)}><Button size="small" icon={<RollbackOutlined />} loading={withdrawing}>撤销</Button></Popconfirm> }]} /></section>
      <Divider />
      <DecisionPanel batch={batch} canDecide={can('release:write') && !pendingPause} onDecide={decide} />
    </div>
  )
}
