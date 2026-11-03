/**
 * 作品展中文文案。
 *
 * 与 `en/iqBenchmark.ts` 的键必须完全一致：
 * `localeKeyCompleteness.spec.ts` 会校验两侧键集相同。
 */
export default {
  iqBenchmark: {
    title: '模型智商检测作品展',
    subtitle: '用同一批题目横向对比各模型的真实产出能力',
    navGallery: '检测作品展',
    navAdmin: '检测作品展管理',

    tab: {
      gallery: '作品展',
      overview: '模型对比',
    },

    filter: {
      model: '模型',
      modelAll: '全部模型',
      case: '题目',
      caseAll: '全部题目',
      status: '状态',
      statusAll: '全部状态',
      kind: '类型',
      kindAll: '全部类型',
      since: '时间范围',
      sinceAll: '不限时间',
      sinceDays: '最近 {n} 天',
      reset: '清除筛选',
      refresh: '刷新',
      search: '搜索模型…',
    },

    kind: {
      image: '图片生成',
      text: '文本生成',
      code: '代码生成',
      image_url: '图片',
      svg: 'SVG 图形',
      markdown: 'Markdown',
    },

    // 题目按判分口径分两组：logic 有参考答案可判，render 只出图不判分。
    // 分组展示是为了不让「通过率」跨组混算 —— 两组的 verdict 口径不同。
    caseGroup: {
      logic: '判分题',
      render: '生成题',
    },

    status: {
      success: '成功',
      failed: '失败',
      timeout: '超时',
      blocked: '已拦截',
      running: '执行中',
      partial: '部分成功',
      idle: '待执行',
      quota_exhausted: '上游配额耗尽',
      budget_exhausted: '超出每日预算',
      account_disabled: '内部账号不可用',
      paused_by_admin: '管理员已暂停',
    },

    trigger: {
      schedule: '定时',
      manual: '手动',
      retry: '重试',
    },

    /** 启用状态通用标签（避免依赖 common 命名空间里不存在的键）。 */
    state: {
      on: '已启用',
      off: '已停用',
    },

    card: {
      contentTruncated: '内容已截断',
      unsafe: '内容已被安全策略拦截',
      noPreview: '暂无内容',
      viewDetail: '查看详情',
      compare: '加入对比',
      compared: '已加入对比',
      score: '评分',
      duration: '耗时',
      cost: '成本',
      tokens: 'Token',
      inputTokens: '输入',
      outputTokens: '输出',
      createdAt: '生成于',
    },

    detail: {
      title: '作品详情',
      prompt: '题目要求',
      content: '模型产出',
      rawUnavailable: '该产出被安全策略拦截，正文不予展示。',
      metadata: '元信息',
      close: '关闭',
    },

    compare: {
      title: '作品对比',
      selected: '已选 {n} 件',
      limit: '最多可同时对比 {n} 件',
      clear: '清空选择',
      start: '开始对比',
      empty: '从作品展中挑选 2 件以上作品进行对比',
      remove: '移出对比',
      maxReached: '已达到对比上限',
    },

    overview: {
      totalArtifacts: '作品总数',
      imageArtifacts: '图片作品',
      textArtifacts: '文本作品',
      caseCount: '题目数',
      modelBreakdown: '模型表现',
      model: '模型',
      total: '总数',
      successRate: '成功率',
      avgDuration: '平均耗时',
      lastRunAt: '最近生成',
      empty: '暂无统计数据',
    },

    matrix: {
      title: '监控矩阵',
      subtitle: '跟随上方筛选条件',
      caseAxis: '题目',
      modelAxis: '模型',
      empty: '当前筛选条件下没有可展示的数据',
      truncated: '数据超过 60 格，当前只展示 {shown} 格',
      state: {
        passed: '通过',
        incorrect: '答错',
        error: '失败',
        empty: '无',
      },
      legend: {
        passed: '通过',
        incorrect: '答错',
        error: '失败',
        empty: '无数据',
      },
      tooltip: {
        base: '{case} × {model}：{state}',
        empty: '{case} × {model}：暂无产出',
      },
    },

    verdict: {
      label: '判定',
      state: {
        ok: '通过',
        degraded: '降级',
        generated: '自由发挥',
        unknown: '未判定',
        normal: '正常',
      },
      errorKind: {
        auth_failed: '鉴权失败',
        forbidden: '无权访问',
        not_found: '模型不存在',
        timeout: '超时',
        rate_limited: '被限流',
        gateway_timeout: '网关超时',
        server_error: '上游 5xx',
        generic: '其他错误',
        stream_broken: '流中断',
        response_incomplete: '响应不完整',
        quota_exceeded: '额度耗尽',
        empty_response: '空响应',
      },
    },

    wall: {
      title: '分组作品墙',
      unnamed: '未命名分组',
      hidden: '该分组未开放展示',
    },

    empty: {
      artifacts: '暂无作品，等待首次检测完成',
      filtered: '当前筛选条件下没有作品',
      cases: '暂无题目',
      tasks: '暂无任务',
      runs: '暂无执行记录',
    },

    error: {
      loadFailed: '加载失败，请稍后重试',
      rateLimited: '请求过于频繁，请 {n} 秒后重试',
      rateLimitedShort: '请求过于频繁',
      disabled: '该功能当前未开启',
      cooldown: '冷却中，请 {n} 秒后再试',
      retry: '重试',
    },

    /** 作品来源角标。 */
    source: {
      scheduled: '定时检测',
      user_test: '用户自测',
      /** 筛选下拉用。 */
      label: '来源',
      all: '全部来源',
    },

    /** 用户自测（用自己本站的 Key 跑一次检测）。 */
    userTest: {
      title: '自己测一次',
      subtitle: '用你自己的 API Key 跑同一批题目，结果会进入作品展并标记「用户自测」。',
      action: '用我的 Key 自测',
      submitting: '提交中…',
      submitted: '已提交，正在后台执行',
      dialogTitle: '选择 API Key 与目标',
      apiKey: 'API Key',
      apiKeyPlaceholder: '请选择一把可用的 Key',
      apiKeyEmpty: '你还没有可用的 API Key',
      apiKeyEmptyHint: '请先到「API 密钥」页面创建一把，额度与过期时间由你自己控制。',
      apiKeyDisabled: '已停用',
      case: '题目',
      casePlaceholder: '请选择题目',
      renderOnlyHint: '这道题只出图、不判分：结果会标为「已生成」，通过率不会把它计入。',
      model: '模型',
      modelPlaceholder: '选择该分组可用的模型',
      group: '分组',
      groupBoundToKey: '该 Key 已绑定分组，分组不可更改。',
      submit: '开始自测',
      cancel: '取消',
      history: '我的自测记录',
      historyEmpty: '还没有自测记录',
      openArtifact: '查看产出',
      costNotice: '自测消耗的是你自己的 Key 额度，费用计入你的账户。',
      reviewNotice: '本次产出需要管理员审核后才会出现在公开作品展。',
      keyFieldNote: 'Key 只在服务端读一次，不会回显，也不会写进日志。',
      /** 各状态文案。 */
      status: {
        pending: '排队中',
        running: '执行中',
        success: '已完成',
        failed: '失败',
        blocked: '已拦截',
        rejected: '已拒绝',
      },
    },

    /** 自测相关的错误文案（按后端 reason 映射）。 */
    userTestError: {
      USER_TEST_DISABLED: '自测功能当前未开启。',
      CASE_NOT_OPEN_FOR_USER_TEST: '该题目未开放自测。',
      USER_NOT_ACTIVE: '你的账号当前不可用，无法发起自测。',
      /** 归属校验失败与「不存在」共用一句文案：避免 Key 存在性泄露。 */
      API_KEY_INACTIVE: '这把 API Key 已停用。',
      API_KEY_EXPIRED: '这把 API Key 已过期，请更换。',
      INSUFFICIENT_QUOTA: '这把 Key 的额度不足，无法发起自测。',
      GROUP_IMAGE_DISABLED: '该分组未开启生图能力，无法用于图片类题目。',
      TARGET_INVALID: '所选分组或模型已不可用，请重新选择。',
      DAILY_LIMIT: '今日自测次数已达上限，请明天再试。',
      CONCURRENT_LIMIT: '还有自测正在执行，请等它结束后再试。',
      DUPLICATE: '同样的自测刚刚已提交过，请稍后再试。',
      BUDGET_EXCEEDED: '今日自测成本已达上限。',
      NOT_FOUND: '记录不存在。',
      UNKNOWN: '自测失败，请稍后重试。',
    },

    admin: {
      title: '模型智商检测作品展管理',
      /** 管理端用户列表的「隐藏内部账号」开关文案。 */
      users: {
        hideInternal: '隐藏内部账号',
      },
      tab: {
        tasks: '检测任务',
        cases: '题目',
        artifacts: '作品',
        settings: '设置',
      },
      disabledNotice: '功能未启用：作品展页面与检测调度都不会运行。',
      enableHint: '在「设置」中开启后，用户侧作品展才会出现。',

      settings: {
        enabled: '启用功能',
        enabledHint: '关闭后用户侧页面不可访问，调度器停止执行。',
        defaultGroupId: '默认分组 ID',
        defaultGroupIdHint: '内部账号绑定的目标分组；留空则使用第一个可用分组。',
        gatewayBaseUrl: '网关地址',
        gatewayBaseUrlHint: '留空则使用本机回环地址 http://127.0.0.1:<端口>。',
        modelTimeout: '单次调用超时（秒）',
        maxArtifactBytes: '单件产出上限（字节）',
        maxArtifactsPerRun: '单次执行最多作品数',
        maxTaskConcurrency: '并发任务上限',
        dailyCostBudget: '每日成本预算（元）',
        dailyCostBudgetHint: '超出后所有任务熔断，次日自动恢复。',
        minBalanceFloor: '最低余额阈值',
        scoringEnabled: '启用评分',
        exposeCost: '向用户展示成本',
        exposeTokens: '向用户展示 Token',
        retentionDays: '作品保留天数',
        maxArtifacts: '作品总量上限',
        cleanupInterval: '清理间隔（分钟）',
        readLimit: '浏览接口限流（次/分）',
        heavyLimit: '对比接口限流（次/分）',
        maxConcurrent: '单用户并发上限',
        save: '保存设置',
        saved: '设置已保存',
        readonly: '仅管理员可修改设置',

        /** 用户自测相关设置。 */
        userTestEnabled: '允许用户自测',
        userTestEnabledHint: '用户可用自己的 API Key 跑检测，结果计入作品展。关闭后自测入口不可用。',
        userTestDailyLimit: '每用户每日自测次数',
        userTestDailyLimitHint: '按自然日计算，跨天后重置。',
        userTestMaxConcurrent: '每用户同时执行数',
        userTestMaxConcurrentHint: '超出后新的自测会被拒绝，直到前面的执行结束。',
        userTestRequireReview: '自测产出需审核',
        userTestRequireReviewHint: '开启后自测产出先进待审队列，审核通过才出现在公开作品展。',
        userTestDailyCostBudget: '每用户每日成本上限（元）',
        userTestDailyCostBudgetHint: '填 0 表示不限制。非 0 时超出即拒绝新的自测。',
      },

      tasks: {
        create: '新建任务',
        edit: '编辑任务',
        name: '任务名称',
        group: '分组',
        model: '模型',
        cases: '题目',
        casesSelected: '已选 {n} 道题',
        interval: '执行间隔（分钟）',
        jitter: '随机抖动（秒）',
        enabled: '启用',
        status: '状态',
        nextRun: '下次执行',
        lastRun: '上次执行',
        runNow: '立即执行',
        runStarted: '已触发执行',
        delete: '删除',
        deleteConfirm: '确定删除该任务？执行记录会保留。',
        intervalHint: '取值范围 {min} - {max} 分钟。',
        jitterHint: '每次调度在前述间隔基础上随机提前 0 - {n} 秒。',
        enableToRecover: '启用可解除熔断状态',
        duplicate: '该分组与模型的组合已存在启用中的任务',
        selectModel: '请选择模型',
        noTargets: '暂无可用的分组',
        orphanGroupName: '分组 #{id}（已停用或不存在）',
        orphanModelHint:
          '模型 {model} 已不在该分组的可用集合内。保存会被拒绝；请改选一个可用模型，或先到分组配置里恢复该模型。',
      },

      cases: {
        create: '新建题目',
        edit: '编辑题目',
        name: '题目名称',
        kind: '题目类型',
        prompt: '提示词',
        systemPrompt: '系统提示词',
        expectKind: '期望产出',
        scoring: '评分方式',
        enabled: '启用',
        sortOrder: '排序',
        builtin: '内置',
        allowUserTest: '开放自测',
        allowUserTestHint: '开启后用户可用自己的 Key 跑这道题。默认关闭：自测消耗的是用户自己的额度。',
        delete: '删除',
        deleteConfirm: '确定删除该题目？',
        builtinUndeletable: '内置题目不可删除',
        inUse: '该题目正被启用中的任务使用，无法删除',
        paramsHint: '可选，JSON 格式的额外请求参数。',
        expected: '参考答案',
        expectedHint:
          '判分的唯一依据：模型回答经归一化后与此处等值即判为通过。留空则该题不可判分，通过率不会把它计入。',
        expectedRequired: '文本/代码题必须填写参考答案，否则无法判分。',
      },

      artifacts: {
        filters: '筛选',
        run: '执行',
        case: '题目',
        model: '模型',
        status: '状态',
        errorCode: '错误码',
        errorSummary: '错误说明',
        unsafeFlags: '危险构造',
        blobKey: '存储键',
        source: '来源',
        sourceAll: '全部来源',
        moderation: {
          label: '审核状态',
          all: '全部审核状态',
          approved: '已通过',
          pending: '待审核',
          rejected: '已拒绝',
        },
        triggeredBy: '触发用户',
        approve: '通过审核',
        reject: '拒绝',
        cost: '成本',
        delete: '删除',
        deleteConfirm: '确定删除该作品？',
        score: '评分',
        scorePrompt: '输入 0 - 100 的分数',
        scoreSaved: '评分已保存',
      },

      runs: {
        title: '执行记录',
        trigger: '触发方式',
        startedAt: '开始时间',
        finishedAt: '结束时间',
        counts: '成功/失败/超时/拦截',
        cost: '成本',
        status: '状态',
      },

      /** 目标级联下拉的空集合/警告成因文案。 */
      targets: {
        reason: {
          NO_SCHEDULABLE_ACCOUNT: '该分组当前没有可调度的账号，任务会一直跑不出结果。',
          IMAGE_GENERATION_DISABLED: '该分组未开启生图能力，无法用于图片类题目。',
          ALLOWLIST_FILTERED_EMPTY: '该分组的模型白名单过滤后为空，请联系管理员补齐白名单。',
          NO_MODEL_MAPPING: '该分组下的账号尚未配置模型映射，且平台没有默认模型可回退。',
        },
      },
    },
  },
}
