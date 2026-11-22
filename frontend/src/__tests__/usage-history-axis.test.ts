import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import UsageHistory from '@/components/UsageHistory.vue'
import NetTile from '@/components/NetTile.vue'

describe('Skala links neben den Verlaufsgrafen', () => {
  it('Prozessor/Arbeitsspeicher: 100, 50, 0 (Einheit % einmal unter dem Graphen)', () => {
    const w = mount(UsageHistory, { props: { history: [10, 40, 20], from: '#000', to: '#fff' } })
    expect(w.findAll('[data-test="history-axis"] span').map((x) => x.text())).toEqual(['100', '50', '0'])
    expect(w.find('[data-test="history-unit"]').text()).toBe('%')
  })

  it('ohne Messwerte keine Skala', () => {
    const w = mount(UsageHistory, { props: { history: [10], from: '#000', to: '#fff' } })
    expect(w.find('[data-test="history-axis"]').exists()).toBe(false)
  })

  it('Netzwerk: Skala wächst mit dem Spitzenwert, mindestens 1 MB/s', () => {
    const w = mount(NetTile, { props: { current: null, rx: [100, 200, 300], tx: [0, 0, 0] } })
    expect(w.findAll('[data-test="net-axis"] span').map((x) => x.text())).toEqual(['1', '0,5', '0'])
    expect(w.find('[data-test="net-unit"]').text()).toBe('MB/s')
  })
})
