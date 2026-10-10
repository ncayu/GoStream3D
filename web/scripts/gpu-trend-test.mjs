import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import ts from 'typescript'

let option, refs
globalThis.ResizeObserver = class { observe() {} disconnect() {} }
globalThis.getComputedStyle = () => ({ getPropertyValue: () => '' })
const container = { closest: () => null }
const source = await readFile(new URL('../src/components/MonitorTrendChart.tsx', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX } }).outputText
const exports = {}
new Function('require', 'exports', compiled)(name => {
  if (name === 'react') return { useRef: () => ({ current: refs++ === 0 ? container : null }), useEffect: callback => callback() }
  if (name === 'react/jsx-runtime') return { jsx: (type, props) => ({ type, props }), jsxs: (type, props) => ({ type, props }) }
  if (name === 'echarts/core') return { use() {}, init: () => ({ resize() {}, dispose() {}, setOption: value => { option = value } }) }
  return {}
}, exports)
const render = history => { refs = 0; return exports.default({ history, theme: 'blue-tech', metric: 'gpu' }) }
const point = (i, gpus) => ({ time: new Date(1700000000000 + i * 30000).toISOString(), rx_mbps: 0, tx_mbps: 0, gpus })
const gpu = (id, value) => ({ id, name: 'Same model', utilization_percent: value })
render([point(0, [gpu('a', 66), gpu('b', 40)]), point(1, [gpu('b', 27), gpu('a', 0)]), point(2, []), point(3, [gpu('a', null), gpu('b', 12)])])
assert.equal(option.yAxis.max, 100)
assert.equal(option.series.length, 2)
assert.notEqual(option.series[0].name, option.series[1].name)
assert.deepEqual(option.series[0].data.map(point => point[1]), [66, 0, null, null])
assert.deepEqual(option.series[1].data.map(point => point[1]), [40, 27, null, 12])
assert.ok(option.series.every(series => !series.connectNulls))
render([point(0), point(1, null), point(2, [gpu('a', 58)])])
assert.equal(option.series[0].showSymbol, true, 'one valid GPU sample must be visible after legacy/missing samples')
assert.deepEqual(option.series[0].data.map(point => point[1]), [null, null, 58])
const empty = render([point(0), point(1, [gpu('a', null)])])
assert.ok(empty.props.children[1], 'unsupported GPU data needs an empty-state overlay')
assert.equal(option.tooltip.valueFormatter(null), '暂无数据')
assert.equal(option.tooltip.valueFormatter(0), '0.0%')
console.log('GPU trends preserve UUID identity, real zero, sampling gaps, legacy data, single samples and percentage axes.')
