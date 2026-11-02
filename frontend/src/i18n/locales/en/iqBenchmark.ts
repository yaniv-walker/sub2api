/**
 * IQ benchmark gallery English copy.
 *
 * Keys must stay identical to `zh/iqBenchmark.ts`:
 * `localeKeyCompleteness.spec.ts` asserts both sides expose the same key set.
 */
export default {
  iqBenchmark: {
    title: 'Model IQ Benchmark Gallery',
    subtitle: 'Compare real model output on an identical set of prompts',
    navGallery: 'Benchmark Gallery',
    navAdmin: 'Benchmark Gallery Admin',

    tab: {
      gallery: 'Gallery',
      overview: 'Model comparison',
    },

    filter: {
      model: 'Model',
      modelAll: 'All models',
      case: 'Prompt',
      caseAll: 'All prompts',
      status: 'Status',
      statusAll: 'All statuses',
      kind: 'Type',
      kindAll: 'All types',
      since: 'Time range',
      sinceAll: 'Any time',
      sinceDays: 'Last {n} days',
      reset: 'Clear filters',
      refresh: 'Refresh',
      search: 'Search models…',
    },

    kind: {
      image: 'Image generation',
      text: 'Text generation',
      code: 'Code generation',
      image_url: 'Image',
      svg: 'SVG graphic',
      markdown: 'Markdown',
    },

    status: {
      success: 'Success',
      failed: 'Failed',
      timeout: 'Timed out',
      blocked: 'Blocked',
      running: 'Running',
      partial: 'Partially succeeded',
      idle: 'Idle',
      quota_exhausted: 'Upstream quota exhausted',
      budget_exhausted: 'Daily budget exceeded',
      account_disabled: 'Internal account unavailable',
      paused_by_admin: 'Paused by admin',
    },

    trigger: {
      schedule: 'Scheduled',
      manual: 'Manual',
      retry: 'Retry',
    },

    /** Generic enable-state labels (the common namespace has no such key). */
    state: {
      on: 'Enabled',
      off: 'Disabled',
    },

    card: {
      contentTruncated: 'Content truncated',
      unsafe: 'Content was blocked by the safety policy',
      noPreview: 'No content',
      viewDetail: 'View details',
      compare: 'Add to comparison',
      compared: 'Added to comparison',
      score: 'Score',
      duration: 'Duration',
      cost: 'Cost',
      tokens: 'Tokens',
      inputTokens: 'Input',
      outputTokens: 'Output',
      createdAt: 'Generated at',
    },

    detail: {
      title: 'Artifact details',
      prompt: 'Prompt',
      content: 'Model output',
      rawUnavailable: 'This output was blocked by the safety policy and is not shown.',
      metadata: 'Metadata',
      close: 'Close',
    },

    compare: {
      title: 'Compare artifacts',
      selected: '{n} selected',
      limit: 'You can compare up to {n} artifacts at a time',
      clear: 'Clear selection',
      start: 'Compare',
      empty: 'Pick at least 2 artifacts from the gallery to compare',
      remove: 'Remove from comparison',
      maxReached: 'Comparison limit reached',
    },

    overview: {
      totalArtifacts: 'Total artifacts',
      imageArtifacts: 'Image artifacts',
      textArtifacts: 'Text artifacts',
      caseCount: 'Prompts',
      modelBreakdown: 'Per-model results',
      model: 'Model',
      total: 'Total',
      successRate: 'Success rate',
      avgDuration: 'Avg duration',
      lastRunAt: 'Last generated',
      empty: 'No statistics yet',
    },

    empty: {
      artifacts: 'No artifacts yet — waiting for the first run',
      filtered: 'No artifacts match the current filters',
      cases: 'No prompts yet',
      tasks: 'No tasks yet',
      runs: 'No runs yet',
    },

    error: {
      loadFailed: 'Failed to load, please try again later',
      rateLimited: 'Too many requests, please retry in {n} seconds',
      rateLimitedShort: 'Too many requests',
      disabled: 'This feature is currently disabled',
      cooldown: 'Cooling down, retry in {n} seconds',
      retry: 'Retry',
    },

    /** Artifact source badge. */
    source: {
      scheduled: 'Scheduled',
      user_test: 'User self-test',
      /** Filter dropdown labels. */
      label: 'Source',
      all: 'All sources',
    },

    /** User self-test (run one detection with your own API key). */
    userTest: {
      title: 'Test it yourself',
      subtitle:
        'Run the same prompts with your own API key. The result joins the gallery and is tagged as a user self-test.',
      action: 'Self-test with my key',
      submitting: 'Submitting…',
      submitted: 'Submitted — running in the background',
      dialogTitle: 'Choose an API key and a target',
      apiKey: 'API key',
      apiKeyPlaceholder: 'Pick an active key',
      apiKeyEmpty: 'You have no usable API keys yet',
      apiKeyEmptyHint: 'Create one on the API Keys page first; you control its quota and expiry.',
      apiKeyDisabled: 'Disabled',
      case: 'Prompt',
      casePlaceholder: 'Pick a prompt',
      model: 'Model',
      modelPlaceholder: 'Pick a model available in this group',
      group: 'Group',
      groupBoundToKey: 'This key is bound to a group, so the group cannot be changed.',
      submit: 'Start self-test',
      cancel: 'Cancel',
      history: 'My self-tests',
      historyEmpty: 'No self-tests yet',
      openArtifact: 'View artifact',
      costNotice: 'A self-test spends your own key quota and is billed to your account.',
      reviewNotice: 'This artifact appears in the public gallery only after an admin reviews it.',
      keyFieldNote: 'The key is read once server-side; it is never echoed back or written to logs.',
      /** Per-status labels. */
      status: {
        pending: 'Queued',
        running: 'Running',
        success: 'Done',
        failed: 'Failed',
        blocked: 'Blocked',
        rejected: 'Rejected',
      },
    },

    /** Self-test error copy (mapped from the backend reason). */
    userTestError: {
      USER_TEST_DISABLED: 'Self-test is currently disabled.',
      CASE_NOT_OPEN_FOR_USER_TEST: 'This prompt is not open for self-test.',
      USER_NOT_ACTIVE: 'Your account is not active, so a self-test cannot be started.',
      /** Ownership failures share the "not found" copy to avoid key-existence leakage. */
      API_KEY_INACTIVE: 'That API key is disabled.',
      API_KEY_EXPIRED: 'That API key has expired; please pick another.',
      INSUFFICIENT_QUOTA: 'That API key has insufficient quota for a self-test.',
      GROUP_IMAGE_DISABLED: 'This group does not allow image generation, so it cannot run image prompts.',
      TARGET_INVALID: 'The selected group or model is no longer available; please pick again.',
      DAILY_LIMIT: 'Daily self-test limit reached. Try again tomorrow.',
      CONCURRENT_LIMIT: 'Another self-test is still running; try again once it finishes.',
      DUPLICATE: 'The same self-test was just submitted; please try again later.',
      BUDGET_EXCEEDED: "Today's self-test cost budget has been reached.",
      NOT_FOUND: 'Record not found.',
      UNKNOWN: 'Self-test failed, please try again later.',
    },

    admin: {
      title: 'Model IQ Benchmark Gallery Admin',
      /** Admin user-list "hide internal accounts" toggle label. */
      users: {
        hideInternal: 'Hide internal accounts',
      },
      tab: {
        tasks: 'Tasks',
        cases: 'Prompts',
        artifacts: 'Artifacts',
        settings: 'Settings',
      },
      disabledNotice: 'Feature disabled: the gallery page and the detection scheduler are inactive.',
      enableHint: 'Enable it under Settings to make the user-facing gallery appear.',

      settings: {
        enabled: 'Enable feature',
        enabledHint: 'When off, the user page is unreachable and the scheduler stops.',
        defaultGroupId: 'Default group ID',
        defaultGroupIdHint: 'Target group bound to the internal account; empty uses the first available group.',
        gatewayBaseUrl: 'Gateway base URL',
        gatewayBaseUrlHint: 'Empty falls back to the local loopback address http://127.0.0.1:<port>.',
        modelTimeout: 'Per-call timeout (seconds)',
        maxArtifactBytes: 'Max artifact size (bytes)',
        maxArtifactsPerRun: 'Max artifacts per run',
        maxTaskConcurrency: 'Max concurrent tasks',
        dailyCostBudget: 'Daily cost budget',
        dailyCostBudgetHint: 'Exceeding it trips every task; recovery is automatic the next day.',
        minBalanceFloor: 'Minimum balance floor',
        scoringEnabled: 'Enable scoring',
        exposeCost: 'Show cost to users',
        exposeTokens: 'Show tokens to users',
        retentionDays: 'Artifact retention (days)',
        maxArtifacts: 'Max artifacts in total',
        cleanupInterval: 'Cleanup interval (minutes)',
        readLimit: 'Browse rate limit (req/min)',
        heavyLimit: 'Compare rate limit (req/min)',
        maxConcurrent: 'Per-user concurrency cap',
        save: 'Save settings',
        saved: 'Settings saved',
        readonly: 'Only admins can change settings',

        /** User self-test settings. */
        userTestEnabled: 'Allow user self-tests',
        userTestEnabledHint:
          'Users can run detections with their own API keys; results join the gallery. Turning it off removes the entry point.',
        userTestDailyLimit: 'Self-tests per user per day',
        userTestDailyLimitHint: 'Counted per calendar day and reset the next day.',
        userTestMaxConcurrent: 'Concurrent self-tests per user',
        userTestMaxConcurrentHint: 'Beyond this, new self-tests are rejected until earlier ones finish.',
        userTestRequireReview: 'Require review for self-tests',
        userTestRequireReviewHint:
          'When on, self-test artifacts enter a pending queue and only appear publicly after approval.',
        userTestDailyCostBudget: 'Daily cost budget per user',
        userTestDailyCostBudgetHint:
          'Zero means unlimited. When non-zero, new self-tests are rejected once it is reached.',
      },

      tasks: {
        create: 'New task',
        edit: 'Edit task',
        name: 'Task name',
        group: 'Group',
        model: 'Model',
        cases: 'Prompts',
        casesSelected: '{n} prompts selected',
        interval: 'Interval (minutes)',
        jitter: 'Jitter (seconds)',
        enabled: 'Enabled',
        status: 'Status',
        nextRun: 'Next run',
        lastRun: 'Last run',
        runNow: 'Run now',
        runStarted: 'Run triggered',
        delete: 'Delete',
        deleteConfirm: 'Delete this task? Run history is kept.',
        intervalHint: 'Must be between {min} and {max} minutes.',
        jitterHint: 'Each run starts 0 - {n} seconds earlier than the interval.',
        enableToRecover: 'Enabling clears the tripped state',
        duplicate: 'A task for this group and model is already enabled',
        selectModel: 'Select a model',
        noTargets: 'No groups available',
        orphanGroupName: 'Group #{id} (inactive or deleted)',
        orphanModelHint:
          'Model {model} is no longer in this group\'s available set. Saving will be rejected; pick an available model, or restore it in the group configuration first.',
      },

      cases: {
        create: 'New prompt',
        edit: 'Edit prompt',
        name: 'Prompt name',
        kind: 'Prompt type',
        prompt: 'Prompt text',
        systemPrompt: 'System prompt',
        expectKind: 'Expected output',
        scoring: 'Scoring',
        enabled: 'Enabled',
        sortOrder: 'Sort order',
        builtin: 'Built-in',
        allowUserTest: 'Open for self-test',
        allowUserTestHint:
          'Lets users run this prompt with their own key. Off by default: a self-test spends the user\u2019s own quota.',
        delete: 'Delete',
        deleteConfirm: 'Delete this prompt?',
        builtinUndeletable: 'Built-in prompts cannot be deleted',
        inUse: 'This prompt is used by an enabled task and cannot be deleted',
        paramsHint: 'Optional extra request parameters as JSON.',
      },

      artifacts: {
        filters: 'Filters',
        run: 'Run',
        case: 'Prompt',
        model: 'Model',
        status: 'Status',
        errorCode: 'Error code',
        errorSummary: 'Error summary',
        unsafeFlags: 'Unsafe constructs',
        blobKey: 'Storage key',
        source: 'Source',
        sourceAll: 'All sources',
        moderation: {
          label: 'Review status',
          all: 'All review statuses',
          approved: 'Approved',
          pending: 'Pending review',
          rejected: 'Rejected',
        },
        triggeredBy: 'Triggered by',
        approve: 'Approve',
        reject: 'Reject',
        cost: 'Cost',
        delete: 'Delete',
        deleteConfirm: 'Delete this artifact?',
        score: 'Score',
        scorePrompt: 'Enter a score between 0 and 100',
        scoreSaved: 'Score saved',
      },

      runs: {
        title: 'Run history',
        trigger: 'Trigger',
        startedAt: 'Started',
        finishedAt: 'Finished',
        counts: 'Success/Failed/Timeout/Blocked',
        cost: 'Cost',
        status: 'Status',
      },

      /** Reasons a target group has no selectable models (or is at risk). */
      targets: {
        reason: {
          NO_SCHEDULABLE_ACCOUNT:
            'This group currently has no schedulable account, so runs will keep producing nothing.',
          IMAGE_GENERATION_DISABLED: 'Image generation is disabled for this group, so it cannot run image cases.',
          ALLOWLIST_FILTERED_EMPTY:
            'This group\'s model allowlist filters everything out. Ask an admin to extend the allowlist.',
          NO_MODEL_MAPPING:
            'Accounts in this group have no model mapping configured, and the platform has no default set to fall back to.',
        },
      },
    },
  },
}
