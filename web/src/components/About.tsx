import { ConsoleIcon as Icon } from './ConsoleIcon'

const capabilities = [
  ['服务端渲染', '在部署设备上使用 CPU 或 GPU 加载和渲染 3D 页面，将画面传送到观看者的浏览器，并接收鼠标、键盘等交互操作。'],
  ['独立会话', '1 路对应 1 位用户的独立渲染实例。多位用户可以同时访问同一个观看链接，各自的画面与操作互不干扰。'],
  ['预加载与按需启动', '提前准备指定数量的实例，缩短进入场景的等待时间；配额内的其他会话按需加载。可分别设置总通道数、项目上限和预加载路数。'],
  ['运行管理', '集中管理观看链接、启动和停止渲染，设置 CPU 使用上限，并查看 CPU、内存、磁盘和网络指标及趋势。'],
]

const scenarios = [
  ['数字孪生与智慧园区', '远程查看园区、楼宇和设备的三维场景，开展巡览、展示与交互。'],
  ['工业与能源可视化', '展示工厂、生产线、矿山和能源设施的三维页面与业务大屏。'],
  ['展厅与项目演示', '通过观看链接向客户、参观人员或团队成员展示已部署的 3D 项目。'],
  ['集中部署与远程访问', '把渲染资源集中在服务器上，让不同终端通过浏览器访问场景，减少观看端承担的 3D 渲染工作。'],
]

export function About() {
  return <section className="about-workspace" aria-label="关于 GoStream3D">
    <section className="console-panel about-intro" aria-labelledby="about-product-title">
      <div className="about-product-heading"><span className="about-product-icon"><Icon name="cube" size={28} /></span><div><h2 id="about-product-title">GoStream3D</h2><p>数字孪生流媒体工作台</p></div></div>
      <p>GoStream3D 用于将已有的 3D 网页和可视化大屏转换为可分享、可实时交互的观看链接。管理员统一配置场景与渲染资源，观看者通过浏览器进入，由服务端完成页面渲染和画面传输。</p>
      <p>软件支持 Windows 与 Linux 部署，提供 CPU 软件渲染和 GPU 渲染模式，适合已有 3D 项目的集中运行与远程展示。</p>
    </section>
    <div className="about-columns">
      <section className="console-panel about-details" aria-labelledby="about-functions-title"><h2 id="about-functions-title">主要功能</h2><dl>{capabilities.map(([title, detail]) => <div key={title}><dt>{title}</dt><dd>{detail}</dd></div>)}</dl></section>
      <section className="console-panel about-details" aria-labelledby="about-scenarios-title"><h2 id="about-scenarios-title">使用场景</h2><dl>{scenarios.map(([title, detail]) => <div key={title}><dt>{title}</dt><dd>{detail}</dd></div>)}</dl></section>
    </div>
    <section className="console-panel about-usage" aria-labelledby="about-usage-title"><h2 id="about-usage-title">如何使用</h2><ol><li><strong>配置项目</strong><span>填写原始页面地址，选择渲染方式、分辨率与会话配额。</span></li><li><strong>准备实例</strong><span>启动渲染并等待预加载完成，在项目列表查看就绪状态。</span></li><li><strong>分享访问</strong><span>复制观看链接，用户通过浏览器进入各自的独立会话。</span></li></ol><p>可选择“自适应观看窗口”，让服务端随各位观看者的窗口尺寸调整画面。实际帧率与可运行路数取决于场景复杂度、服务器性能及资源配置。</p></section>
  </section>
}
