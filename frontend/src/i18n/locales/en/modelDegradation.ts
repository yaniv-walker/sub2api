/**
 * Model degradation detection — English copy.
 *
 * Key set must match `zh/modelDegradation.ts` exactly:
 * `localeKeyCompleteness.spec.ts` asserts both sides are identical.
 */
export default {
  modelDegradation: {
    title: 'Model Degradation',
    // Sidebar entry labels (user / admin).
    nav: 'Model Degradation',
    navAdmin: 'Degradation Admin',
    description:
      'Point at your own upstream with your own credentials and find out, right now, whether the model still holds up. Credentials are used for this run only and are never stored.',
    subtitle: 'Bring your own key · judged on the spot',
    running: 'Running',

    monitor: {
      title: 'Recent runs',
      capacity: 'Pass rate {rate} · {n} completed',
      caption: 'Each bar is one run: height is the share of logic tasks passed, colour is the final verdict.',
      empty: 'No runs yet',
      legend: {
        passed: 'Normal',
        incorrect: 'Suspected degradation',
        error: 'Failed or undetermined'
      }
    },

    stats: {
      passRate: 'Pass rate',
      lastDuration: 'Last duration',
      runCount: 'Total runs'
    },

    resultPanel: {
      title: 'Result',
      waiting: 'Nothing selected yet. Fill in the form to start a run.',
      running: 'Working through the tasks',
      retrying: 'Retrying',
      seconds: '{n}s elapsed',
      stop: 'Stop',
      cancelled: 'Stopped',
      cancelledHint: 'This run was stopped on request. Tasks that already finished are kept below.',
      pollExhausted:
        'This is taking a while, so live refresh has stopped. You can reopen the run from the history below.'
    },

    result: {
      expand: 'Expand',
      questionDetail: 'Per-task detail',
      expected: 'Expected',
      answer: 'Answer',
      reason: 'Why',
      copyArtwork: 'Copy source',
      downloadArtwork: 'Download SVG',
      openArtwork: 'Open in new tab',
      artworkCopied: 'Copied'
    },

    form: {
      title: 'Start a run',
      baseUrl: 'Upstream URL',
      baseUrlPlaceholder: 'https://api.example.com/v1',
      baseUrlHint: 'HTTPS only. Private networks and cloud metadata addresses are rejected.',
      apiKey: 'API key',
      apiKeyPlaceholder: 'sk-...',
      apiKeyHint: 'Used for this run only. Never stored, never echoed back.',
      protocolLabel: {
        chat_completions: 'Chat',
        responses: 'Responses',
        messages: 'Messages'
      },
      protocolHint: {
        chat_completions: 'OpenAI-compatible /v1/chat/completions endpoint.',
        responses: 'OpenAI /v1/responses endpoint. Supports render tasks.',
        messages: 'Anthropic /v1/messages endpoint, authenticated with x-api-key.'
      },
      model: 'Model',
      modelHint: 'Pick from the list, or just type the model name.',
      modelPlaceholder: 'gpt-4o / claude-sonnet-4 ...',
      modelCreatablePrefix: 'Will use "{model}" as typed',
      modelFetch: 'Fetch model list',
      modelNeedCredentials: 'Enter URL and key first',
      reasoningEffort: 'Reasoning effort',
      reasoningEffortLevel: {
        low: 'Low',
        medium: 'Medium',
        high: 'High',
        xhigh: 'Very high',
        max: 'Max'
      },
      renderTimeout: 'Render timeout',
      renderTimeoutUnit: 'min',
      renderTimeoutHint: 'Longest wait allowed for a render task. Raise it for complex scenes.',
      tasks: 'Tasks',
      taskLogic: 'Logic',
      taskRender: 'Render',
      presetNone: 'No tasks selected yet',
      customQuestion: 'Add a custom logic task',
      customPromptPlaceholder: 'Write a question with one unambiguous answer, e.g. an arithmetic puzzle…',
      customExpectedPlaceholder: 'Expected answer (used for comparison)',
      taskBankLoading: 'Loading task bank…',
      taskBankEmpty: 'The task bank is unavailable right now. You can still write your own.',
      securityNote:
        'URL and key are used for this run only: the key is never written to the database, and the URL is stored as host only.',
      submit: 'Start',
      submitting: 'Starting…'
    },

    verdict: {
      normal: 'Normal',
      degraded: 'Suspected degradation',
      unknown: 'Undetermined',
      generated: 'Generated'
    },

    verdictHint: {
      normal: 'Every logic task was answered correctly. The model looks healthy.',
      degraded: 'At least one task was answered incorrectly. That may be degradation, or the task may simply be beyond it.',
      unknown:
        'No task produced a usable answer. That usually means an unstable upstream or a misconfiguration, not a weaker model.',
      renderOnly: 'Only render tasks ran, so there is nothing to compare against — no normal/degraded verdict is given.'
    },

    metrics: {
      logic: 'Logic',
      render: 'Render',
      duration: 'Duration',
      protocol: 'Protocol',
      reasoningEffort: 'Reasoning effort',
      reasoningEffortUnset: 'Unset',
      renderTimeout: 'Render timeout',
      renderOk: 'Rendered',
      renderGenerated: 'Generated',
      renderDegraded: 'Render failed',
      renderUnknown: 'Undetermined',
      renderSkipped: 'Not tested'
    },

    item: {
      passed: 'Passed',
      failed: 'Failed',
      unknown: 'No usable answer',
      generated: 'Generated',
    },

    history: {
      title: 'History',
      empty: 'No runs yet. After the first one, they show up here grouped by model.',
      owner: 'Owner',
      ownerSelf: 'Me',
      ownerUnknown: 'Unknown',
      groupLabel: 'Group',
      groupUnknown: 'Unknown group',
      ribbon: {
        passed: 'Passed',
        degraded: 'Degraded',
        unknown: 'Undetermined',
        failed: 'Failed',
        cancelled: 'Stopped',
        running: 'Running',
        retrying: 'Retrying',
        generated: 'Generated'
      },
      typeFilterLabel: 'Task type',
      filterLabel: 'Verdict',
      filter: {
        all: 'All',
        normal: 'Normal',
        degraded: 'Suspected degradation',
        unknown: 'Undetermined',
        generated: 'Generated'
      },
      groupFilter: {
        all: 'All'
      },
      timeline: {
        yesterday: 'Yesterday',
      },
      prevPage: 'Previous',
      nextPage: 'Next',
      pageStatus: 'Page {page} of {total}'
    },

    errors: {
      needBaseUrl: 'Enter the upstream URL',
      needApiKey: 'Enter the API key',
      needModel: 'Enter or pick a model',
      needTask: 'Select at least one task',
      needQuestion: 'A custom task needs a prompt',
      needCustomExpected: 'A custom logic task needs an expected answer',
      needRenderTimeout: 'Render timeout must be between 1 and 60 minutes',
      startFailed: 'Could not start the run. Try again shortly.',
      startInvalid: 'Invalid input. Check the URL, key and tasks.',
      startConflict: 'You already have a run in progress. Wait for it to finish or stop it first.',
      modelsFailed: 'Could not fetch the model list — you can type the model name directly',
      loadTasksFailed: 'Could not load the task bank',
      loadFailed: 'Could not load. Try again shortly.',
      cancelFailed: 'Could not stop the run. Try again shortly.'
    },

    admin: {
      title: 'History visibility',
      subtitle:
        'Control which groups and upstream hosts appear on the public history wall. Hiding is reversible and does not touch the records themselves.',
      loading: 'Loading…',
      hostsTitle: 'Upstream hosts',
      hostsHint:
        'Hides every run under this host from the public wall. Hosts that are hidden but have no runs right now are listed too, so you can unhide them.',
      groupsTitle: 'Hidden groups',
      groupsHint: 'A hidden group does not appear on the public wall, but its runs are kept.',
      noHosts: 'No upstream hosts recorded yet',
      noGroups: 'No hidden groups',
      hiddenCount: '{n} hidden',
      runCount: '{n} runs',
      hidden: 'Hidden',
      visible: 'Public'
    },

    upstream: {
      authFailed: 'Authentication failed: the key is invalid or expired',
      forbidden: 'Not allowed to access this model',
      notFound: 'Endpoint or model not found',
      timeout: 'Upstream timed out',
      rateLimited: 'Upstream rate limit hit',
      quotaExceeded: 'Quota exhausted',
      serverError: 'Upstream service error',
      gatewayTimeout: 'Upstream gateway timeout',
      streamBroken: 'Response stream broke',
      responseIncomplete: 'Response was incomplete',
      emptyResponse: 'Upstream returned empty content',
      generic: 'Upstream request failed'
    }
  }
}
