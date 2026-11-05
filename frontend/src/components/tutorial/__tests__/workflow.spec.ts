import { describe, expect, it, vi, beforeEach } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import type { Tutorial } from "@/api/tutorial";

const api = vi.hoisted(() => ({
  listAdminTutorials: vi.fn(),
  getAdminTutorial: vi.fn(),
  createTutorial: vi.fn(),
  updateTutorial: vi.fn(),
  setTutorialStatus: vi.fn(),
  deleteTutorial: vi.fn(),
  listTutorialRevisions: vi.fn(),
  restoreTutorialRevision: vi.fn(),
  getTutorial: vi.fn(),
}));
const navigation = vi.hoisted(() => ({
  replace: vi.fn(),
  route: { query: {} as Record<string, string>, params: { slug: "first" } },
}));
vi.mock("@/api/tutorial", () => api);
vi.mock("@/stores", () => ({ useAppStore: () => ({ showSuccess: vi.fn() }) }));
vi.mock("@/stores/auth", () => ({
  useAuthStore: () => ({ isAuthenticated: true, isAdmin: true }),
}));
vi.mock("vue-router", () => ({
  useRoute: () => navigation.route,
  useRouter: () => ({ replace: navigation.replace }),
  onBeforeRouteLeave: vi.fn(),
}));
vi.mock("@/components/tutorial/TutorialEditor.vue", () => ({
  default: {
    props: ["modelValue", "tutorialId"],
    template:
      '<div data-editor :data-tutorial-id="tutorialId">{{ modelValue }}</div>',
  },
}));
vi.mock("@/components/tutorial/TutorialSupportSettings.vue", () => ({
  default: { template: "<div />" },
}));
vi.mock("@/components/tutorial/TutorialSupportDialog.vue", () => ({
  default: { template: "<div />" },
}));
vi.mock("@/components/layout/AppLayout.vue", () => ({
  default: { template: "<div><slot /></div>" },
}));
import TutorialsView from "@/views/admin/TutorialsView.vue";

const document = (id: number, version = 1): Tutorial => ({
  id,
  slug: `article-${id}`,
  title: `教程 ${id}`,
  summary: "摘要",
  category: "clients",
  content_markdown: "# 步骤",
  content_html: "<h1>步骤</h1>",
  status: "draft",
  is_public: false,
  sort_order: 1,
  version,
  resources: [],
  created_at: "2026-11-05T00:00:00Z",
  updated_at: "2026-11-05T00:00:00Z",
});
const clickButton = async (wrapper: ReturnType<typeof mount>, text: string) => {
  const button = wrapper.findAll("button").find((x) => x.text() === text);
  expect(button).toBeTruthy();
  await button!.trigger("click");
  await flushPromises();
};

beforeEach(() => {
  vi.clearAllMocks();
  navigation.route.query = {};
  api.listAdminTutorials.mockResolvedValue({
    items: [],
    total: 0,
    pages: 1,
    page: 1,
    page_size: 20,
  });
  navigation.replace.mockResolvedValue(undefined);
});
describe("tutorial editing workflow", () => {
  it("binds a newly saved document so the second save updates it instead of creating a duplicate", async () => {
    api.createTutorial.mockResolvedValue(document(42));
    api.updateTutorial.mockResolvedValue(document(42, 2));
    const wrapper = mount(TutorialsView, {
      global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
    });
    await flushPromises();
    const fields = wrapper.findAll("input");
    await fields[1]!.setValue("教程 42");
    await fields[2]!.setValue("article-42");
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(api.createTutorial).toHaveBeenCalledTimes(1);
    expect(wrapper.find("[data-editor]").attributes("data-tutorial-id")).toBe(
      "42",
    );
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(api.createTutorial).toHaveBeenCalledTimes(1);
    expect(api.updateTutorial).toHaveBeenCalledWith(
      42,
      expect.objectContaining({ version: 1 }),
    );
  });
  it("publishes only after saving and sends the new optimistic version", async () => {
    navigation.route.query = { id: "7" };
    api.getAdminTutorial.mockResolvedValue({
      tutorial: document(7),
      assets: [],
    });
    api.updateTutorial.mockResolvedValue(document(7, 2));
    api.setTutorialStatus.mockResolvedValue({
      ...document(7, 3),
      status: "published",
    });
    const wrapper = mount(TutorialsView, {
      global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
    });
    await flushPromises();
    await clickButton(wrapper, "保存并发布");
    expect(api.setTutorialStatus).toHaveBeenCalledWith(7, "published", 2);
    expect(api.updateTutorial.mock.invocationCallOrder[0]).toBeLessThan(
      api.setTutorialStatus.mock.invocationCallOrder[0]!,
    );
  });
  it("keeps unsaved content on a conflict and does not publish", async () => {
    navigation.route.query = { id: "7" };
    api.getAdminTutorial.mockResolvedValue({
      tutorial: document(7),
      assets: [],
    });
    api.updateTutorial.mockRejectedValue({ status: 409 });
    const wrapper = mount(TutorialsView, {
      global: { stubs: { RouterLink: { template: "<a><slot /></a>" } } },
    });
    await flushPromises();
    await wrapper.findAll("input")[1]!.setValue("我的修改");
    await clickButton(wrapper, "保存并发布");
    expect(wrapper.findAll("input")[1]!.element.value).toBe("我的修改");
    expect(wrapper.text()).toContain("内容已被更新");
    expect(api.setTutorialStatus).not.toHaveBeenCalled();
  });
});
