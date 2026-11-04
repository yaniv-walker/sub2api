/**
 * 模型降智检测的中文文案。
 *
 * 与 `en/modelDegradation.ts` 的键必须完全一致：
 * `localeKeyCompleteness.spec.ts` 会校验两侧键集相同。
 */
export default {
  modelDegradation: {
    title: '模型降智检测',
    description:
      '用你自己的上游地址与密钥，当场测一次这个模型还灵不灵。检测用的凭据只在这一次请求里使用，不会保存。',
    subtitle: '自带凭据 · 当场判定',
    running: '检测中',

    monitor: {
      title: '最近检测',
      capacity: '通过率 {rate} · 已完成 {n} 次',
      caption: '每一根柱子是一次检测，高度是逻辑题通过比例，颜色是最终判定。',
      empty: '暂无记录',
      legend: {
        passed: '正常',
        incorrect: '疑似降智',
        error: '失败或无法判定'
      }
    },

    stats: {
      passRate: '通过率',
      lastDuration: '上次耗时',
      runCount: '累计检测'
    },

    resultPanel: {
      title: '检测结果',
      waiting: '还没有正在查看的检测。填好左边就能开始。',
      running: '正在逐题检测',
      retrying: '正在重试',
      seconds: '已用 {n} 秒',
      stop: '中止',
      cancelled: '已中止',
      cancelledHint: '这次检测被主动中止，已完成的题目仍保留在结果里。',
      pollExhausted: '等待时间较久，页面已停止自动刷新。你可以稍后在下方历史里点回这次检测。'
    },

    result: {
      expand: '展开',
      questionDetail: '逐题明细',
      expected: '期望答案',
      answer: '模型回答',
      reason: '判定说明',
      copyArtwork: '复制源码',
      downloadArtwork: '下载 SVG',
      openArtwork: '新窗口打开',
      artworkCopied: '已复制'
    },

    form: {
      title: '发起检测',
      baseUrl: '上游地址',
      baseUrlPlaceholder: 'https://api.example.com/v1',
      baseUrlHint: '只接受 https。内网地址与云元数据地址会被拒绝。',
      apiKey: 'API 密钥',
      apiKeyPlaceholder: 'sk-...',
      apiKeyHint: '只在这次检测中使用，不落库、不回显。',
      protocolLabel: {
        chat_completions: 'Chat',
        responses: 'Responses',
        messages: 'Messages'
      },
      protocolHint: {
        chat_completions: 'OpenAI 兼容的 /v1/chat/completions 接口。',
        responses: 'OpenAI 的 /v1/responses 接口，支持生成题。',
        messages: 'Anthropic 的 /v1/messages 接口，使用 x-api-key 认证。'
      },
      model: '模型',
      modelHint: '可以从列表里选，也可以直接输入模型名。',
      modelPlaceholder: 'gpt-4o / claude-sonnet-4 ...',
      modelCreatablePrefix: '将直接使用「{model}」',
      modelFetch: '拉取模型列表',
      modelNeedCredentials: '先填地址与密钥',
      reasoningEffort: '思考等级',
      reasoningEffortLevel: {
        low: '低',
        medium: '中',
        high: '高',
        xhigh: '很高',
        max: '最高'
      },
      renderTimeout: '生成超时',
      renderTimeoutUnit: '分钟',
      renderTimeoutHint: '生成题允许的最长等待时间，画复杂场景时建议放大。',
      tasks: '检测项',
      taskLogic: '逻辑题',
      taskRender: '生成题',
      presetNone: '还没有选题目',
      customQuestion: '加一道自定义逻辑题',
      customPromptPlaceholder: '写一道有唯一答案的题，例如一个需要计算的数学题……',
      customExpectedPlaceholder: '期望答案（用于比对）',
      taskBankLoading: '正在加载题库……',
      taskBankEmpty: '题库暂时不可用，你可以写自定义题。',
      securityNote:
        '地址与密钥只用于这一次检测：密钥不写入数据库，地址只保留域名部分用于历史展示。',
      submit: '开始检测',
      submitting: '正在发起……'
    },

    verdict: {
      normal: '正常',
      degraded: '疑似降智',
      unknown: '无法判定',
      generated: '已生成'
    },

    verdictHint: {
      normal: '逻辑题全部答对，这个模型目前状态正常。',
      degraded: '有题目答错了。可能是模型能力下降，也可能是这道题本身超出它的能力。',
      unknown: '所有题目都没拿到有效回答，通常说明上游不稳定或配置有误，而不是模型变笨了。',
      renderOnly: '这次只跑了生成题，没有逻辑题可比对，因此不给出「正常/降智」结论。'
    },

    metrics: {
      logic: '逻辑',
      render: '生成',
      duration: '耗时',
      protocol: '协议',
      reasoningEffort: '思考等级',
      reasoningEffortUnset: '未指定',
      renderTimeout: '生成超时',
      renderOk: '生成成功',
      renderGenerated: '已生成',
      renderDegraded: '生成失败',
      renderUnknown: '无法判定',
      renderSkipped: '未检测'
    },

    item: {
      passed: '通过',
      failed: '未通过',
      unknown: '无有效回答',
      generated: '已生成',
    },

    history: {
      title: '检测历史',
      empty: '还没有检测记录。发起第一次检测后，这里会按模型分组展示。',
      owner: '发起人',
      ownerSelf: '我',
      ownerUnknown: '未知',
      groupLabel: '分组',
      groupUnknown: '未知分组',
      ribbon: {
        passed: '通过',
        degraded: '降智',
        unknown: '无法判定',
        failed: '失败',
        cancelled: '已中止',
        running: '检测中',
        retrying: '重试中',
        generated: '已生成'
      },
      typeFilterLabel: '题型',
      filterLabel: '判定',
      filter: {
        all: '全部',
        normal: '正常',
        degraded: '疑似降智',
        unknown: '无法判定',
        generated: '已生成'
      },
      groupFilter: {
        all: '全部'
      },
      timeline: {
        yesterday: '昨天',
      },
      prevPage: '上一页',
      nextPage: '下一页',
      pageStatus: '第 {page} / {total} 页'
    },

    errors: {
      needBaseUrl: '请填写上游地址',
      needApiKey: '请填写 API 密钥',
      needModel: '请填写或选择模型',
      needTask: '请至少选一道题',
      needQuestion: '自定义题需要填写题干',
      needCustomExpected: '自定义逻辑题需要填写期望答案',
      needRenderTimeout: '生成超时需在 1 到 60 分钟之间',
      startFailed: '发起失败，请稍后重试',
      startInvalid: '参数不合法，请检查地址、密钥与题目',
      startConflict: '你已有一次检测正在进行，等它结束或先中止',
      modelsFailed: '拉取模型列表失败，可以直接手动输入模型名',
      loadTasksFailed: '题库加载失败',
      loadFailed: '加载失败，请稍后重试',
      cancelFailed: '中止失败，请稍后重试'
    },

    admin: {
      title: '检测历史可见性',
      subtitle: '控制哪些分组与上游域名会出现在公开的历史墙上。隐藏可逆，不影响记录本身。',
      loading: '正在加载……',
      hostsTitle: '上游域名',
      hostsHint: '把这个域名下的所有检测从公开墙收起。已隐藏但暂无记录的主机也会列出来，方便取消隐藏。',
      groupsTitle: '已隐藏的分组',
      groupsHint: '被隐藏的分组不会出现在公开墙，但分组内的记录仍然保留。',
      noHosts: '还没有任何上游域名记录',
      noGroups: '没有隐藏任何分组',
      hiddenCount: '已隐藏 {n} 个',
      runCount: '{n} 次检测',
      hidden: '已隐藏',
      visible: '公开'
    },

    upstream: {
      authFailed: '认证失败：密钥无效或已过期',
      forbidden: '没有权限访问这个模型',
      notFound: '接口或模型不存在',
      timeout: '上游响应超时',
      rateLimited: '触发上游限流',
      quotaExceeded: '额度已用尽',
      serverError: '上游服务异常',
      gatewayTimeout: '上游网关超时',
      streamBroken: '响应流中断',
      responseIncomplete: '响应内容不完整',
      emptyResponse: '上游返回了空内容',
      generic: '上游请求失败'
    }
  }
}
