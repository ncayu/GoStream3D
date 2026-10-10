# Linux GPU 远景边缘与拖动闪烁优化

2026-10-10。针对园区 ThingJS 场景中远处幕墙、屋顶与相邻表面出现的跳动白条、破碎边缘进行修复。

## 排查依据

对比 NVIDIA 服务器 Chromium 的原始截图与 WebRTC 观看画面，原始画面也存在细密结构异常。测试接收 1920×1080 H.264 / 30 FPS 视频，583 个解码帧没有丢帧或丢包；这支持先排查渲染阶段，但不能仅凭这次采样排除所有网络问题。

实际 WebGL 默认画布为 24 位深度、4 个采样；然而 ThingJS 后期效果创建的离屏缓冲仅为 **16 位深度**，其中多重采样缓冲实际使用 8 个采样。默认画布抗锯齿正常，并不代表后期渲染的深度精度足够。远处相邻表面会落入同一个深度区间，产生随视角变化的深度冲突（z-fighting）。[Three.js 官方说明](https://threejs.org/manual/pages/cameras.html)也解释了这种精度不足造成的显示冲突。

同一摄像机位置、同一 1920×1080 画布和同一采样数量下，只把离屏深度从 16 位改成 24 位，幕墙边缘、连廊细线和相邻表面的异常明显减少。原始配对截图保存在 `output/ghost-diagnostics/depth16-before.png`、`depth24-after.png` 和 `front-roof-16.png`、`front-roof-24.png`。

## 修复方式

- 在 Linux Docker / 原生 Linux 的 GPU 模式中，对所有有效 HTTP/HTTPS 场景链接接入，支持公网平台与内网平台，已取消 ThingJS 域名限制。本次实机验证使用 NVIDIA；Intel 后端共享入口，本次未复测 Intel 硬件。
- 浏览器先启动空白页，安装深度精度脚本，再首次加载目标页面；避免首帧使用旧精度。
- WebGL2 的 16 位深度专用缓冲升级为 24 位，保留宽、高及原有采样数量。不改颜色、贴图、模型、相机裁剪距离或材质。
- 多重采样格式先检查驱动支持的实际采样数量。原格式与新格式不能产生相同采样数量时，保持原格式，避免黑屏或静默降低抗锯齿。
- 脚本连接随渲染实例保持，覆盖页面刷新、跨站导航与后续窗口缩放；跨域 iframe 获得独立初始化，嵌套 iframe 同样接入。停止或启动回滚时释放连接。
- CPU、Windows 本机模式、WebGL1、颜色缓冲、已有 24/32 位深度与深度/模板组合格式保持原行为。深度纹理不重写，避免改变其数据类型和源引擎的纹理处理约定。

这次修复没有降低分辨率，没有加入历史帧混合，没有改变 NVENC 编码预算，也没有改成全屏模糊滤镜。24 位缓冲可能增加显存或显存带宽需求，需要结合最终硬件复测；采样精度增加并不代表所有模型都没有几何重叠问题。

## 已完成的开发验证

NVIDIA RTX 3050 Ti Laptop / WSL2 上确认：深度缓冲为 24 位、后期多重采样仍为 8；窗口从 1920×1080 改为 1600×900 再恢复后，两者保持正确，WebGL 错误为 0。颜色修正、MSAA 与原场景配置保留，未启用 temporalSuperSampling。诊断 JSON 为 `output/ghost-diagnostics/profile-verification.json`。

回归测试覆盖启动时先安装再导航、GPU/CPU 与目标域名范围、采样数量向上取整、驱动不支持的采样组合、幂等安装、其他缓冲格式及无 WebGL2 环境。前端测试与构建、网关 Go 测试、worker 画质预算测试、CPU 容器预加载和重建后的数据持久化回归通过。

## 初次园区修复验收（20261010-100544）

成套网关与 worker 版本 **20261010-100544** 已导入服务器 `192.168.1.100`，网关健康。使用独立临时项目测试，原项目保持停止状态，原项目配置、`.env` 与后台配置文件保持一致。

确认园区模型已完整加载后，场景一次绘制约 **215 万三角形**。1920×1080 H.264 连续拖动测试 12.934 秒，接收解码 357 帧，平均 **27.60 FPS**，视频载荷约 **18.06 Mbps**，丢帧 0、丢包 0、前端脚本异常 0。该测试用于证明实际交互可用，未进行相同负载的旧版本帧率配对，不作为提升帧率百分比的证据。页面刷新后及渲染尺寸变化后，离屏缓冲仍为 24 位深度、8 个采样，WebGL 错误为 0。

截图与 JSON 随该版本（20261010-100544）Docker 离线包的 `depth-proof/` 一同保存，摘要为 `DEPTH_VERIFICATION.json`。临时项目已删除。原链接当前仍为停止状态，点击“重新预加载”或启动渲染后，新建实例会使用新设置。

部署前备份位于 `/root/GoStrem3D/.diagnostics/ghost-20261010-095023/`；旧镜像保留 `gostream3d/gateway:before-depth24-20261010`、`gostream3d/neko/chromium:before-depth24-20261010` 与 `gostream3d/neko/nvidia-chromium:before-depth24-20261010` 标签。需要回退时先将这些旧镜像恢复对应的默认标签，再执行 `docker-compose -f docker-compose.yml up -d --force-recreate gateway`。

验证的是该设备与该园区场景。没有承诺所有场景零闪烁或稳定 4K60；完全共面的重复模型仍可能需要源模型调整。

## 跨平台场景支持（后续扩展）

通用处理位于 WebGL2 缓冲分配层，使用标准 WebGL API，不依赖 ThingJS、Three.js 的私有对象或某个平台的相机接口。主线程 OffscreenCanvas 的 WebGL2 上下文也能接入。独立进程 iframe 在执行场景脚本之前完成初始化；初始化失败时释放暂停状态并记录日志，避免让嵌入页面一直冻结。

兼容性测试发现：将 24 位深度缓冲复制到原有 16 位深度纹理时，WebGL 会报 `INVALID_OPERATION`，影响依赖深度的后期效果。因此，当某个上下文分配 16 位深度纹理时，保留它的纹理格式和数据类型，并将该上下文已升级的缓冲恢复为原 16 位及原采样数量，后续分配也保持一致。恢复后的深度内容由源引擎随后正常绘制填充；这类管线优先保留兼容性，需要结合实际引擎再优化其深度纹理。检测覆盖 `texImage2D/3D` 与 `texStorage2D/3D`，不修改其他上下文，也不拦截每帧绘制。标准依据见 [WebGL2 规范的帧缓冲复制接口](https://registry.khronos.org/webgl/specs/2.0/#3.7.4)。

新增独立 WebGL2 回归场景，不使用 ThingJS SDK。两片距离接近的表面在 16 位深度下会发生错误遮挡（像素为红色）；升级后读取到正确的近处表面（绿色）。实际浏览器测试直接调用生产 Go/CDP 控制器，覆盖首次导航、刷新、尺寸变化、切换域名与跨域 iframe；同时检查帧缓冲完整性、原采样数量、颜色与深度/模板格式、WebGL1 及深度纹理兼容性。结果见 `output/generic-depth/browser-regression.json`。

运行方式：安装 Playwright 后，在 `web` 目录执行 `npm run test:depth:browser`。可通过 `PLAYWRIGHT_MODULE_PATH` 指定模块路径，`PLAYWRIGHT_CHANNEL` 指定 Chromium 浏览器渠道（默认 Edge）。基础回归仍由 `npm test` 和 `go test ./internal/orchestrator` 运行。

这是对相同渲染问题的通用防护，并不等于已逐个验证所有 3D 平台。使用 WebGPU、WebGL1、Worker 内渲染或深度纹理的路径保持原行为；这些路径的画质问题需要按实际平台进一步诊断。平台自身的时域抗锯齿拖影、模型重叠、贴图精度或复杂场景性能也需要分别处理。

## 通用版本服务器验收（20261010-104447）

新版本已导入 `192.168.1.100`，在 RTX 3050 Ti Laptop / WSL2 上使用独立的非 ThingJS WebGL2 测试页验证。首次加载、刷新、尺寸变化、跨域 iframe 与嵌套 iframe 均为 24 位深度，像素遮挡正确，WebGL 错误为 0。请求 2/4/5 个采样时分别保持驱动实际 2/4/8 个采样，帧缓冲完整；16 位深度纹理复制组合保留 16 位匹配格式，复制错误为 0。原有颜色、32 位深度和深度/模板格式保持一致。

1920×1080 H.264 接收测试约 5 秒，解码 135 帧，平均 26.90 FPS，丢帧 0、丢包 0、前端异常 0。这是轻量独立场景的链路兼容性检查，不是复杂模型或其他商业 3D 平台的性能基准。JSON 记录为 `output/generic-depth/nvidia-acceptance.json`。

原 ThingJS 园区同时完成回归：215 万三角形完整加载后，离屏缓冲仍为 24 位，原 8 倍多重采样保留，尺寸变化后结果一致，WebGL 错误为 0。本场景未触发 16 位深度纹理兼容回退；没有丢失此前的园区精度优化。记录为 `output/generic-depth/thingjs-regression.json`。

测试项目和 HTTP 测试服务均已清理，原项目配置与停止状态、`.env` 和后台配置文件保持一致，网关健康。该验收版本的成套离线包为 `GoStream3D-docker-compose-linux-amd64-20261010-104447.zip`，内含 `generic-depth-proof/` 验证记录和 `GPU_GENERIC_VERIFICATION.json` 摘要。备份位于 `/root/GoStrem3D/.diagnostics/generic-depth-20261010-1036/`，部署前镜像保留 `before-generic-depth-20261010` 标签。

## Docker Hub 发行（20261010-110721）

本次发行重新构建网关和通用 Worker，配套标签使用 20261010-110721。源码测试与 CPU 容器回归由本次 VERIFICATION.json 记录。随包 generic-depth-proof/ 与 GPU_GENERIC_VERIFICATION.json 保留 20261010-104447 的 NVIDIA 实机证据及其原始版本号；它们属于历史验收，不表示本次镜像已重新部署或复测所有 GPU。HISTORICAL_GPU_EVIDENCE.json 记录深度优化源码与验收版本的哈希比对。
