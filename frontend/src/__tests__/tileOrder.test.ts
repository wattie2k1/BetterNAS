import { beforeEach, describe, expect, it } from 'vitest'
import { mergeOrder, moveTile, shiftTile, useTileOrder } from '@/utils/tileOrder'

const ids = ['a', 'b', 'c', 'd']

describe('tileOrder', () => {
  it('gleicht gespeicherte Reihenfolge mit den bekannten Kacheln ab', () => {
    expect(mergeOrder(['c', 'a'], ids)).toEqual(['c', 'a', 'b', 'd'])
    expect(mergeOrder(['x', 'c', 'c', 7, 'a'], ids)).toEqual(['c', 'a', 'b', 'd'])
    expect(mergeOrder(null, ids)).toEqual(ids)
    expect(mergeOrder('kaputt', ids)).toEqual(ids)
  })

  it('verschiebt auf die Zielposition', () => {
    expect(moveTile(ids, 'a', 'c')).toEqual(['b', 'c', 'a', 'd'])
    expect(moveTile(ids, 'd', 'a')).toEqual(['d', 'a', 'b', 'c'])
    expect(moveTile(ids, 'a', 'a')).toEqual(ids)
    expect(moveTile(ids, 'x', 'a')).toEqual(ids)
  })

  it('verschiebt um Plätze unter den sichtbaren Kacheln', () => {
    expect(shiftTile(ids, ids, 'b', -1)).toEqual(['b', 'a', 'c', 'd'])
    expect(shiftTile(ids, ids, 'a', -1)).toEqual(ids)
    expect(shiftTile(ids, ids, 'd', 1)).toEqual(ids)
    // „b“ ist ausgeblendet: „c“ rückt hinter „d“ statt hinter das unsichtbare „b“.
    expect(shiftTile(ids, ['a', 'c', 'd'], 'c', 1)).toEqual(['a', 'b', 'd', 'c'])
  })
})

describe('useTileOrder', () => {
  beforeEach(() => localStorage.clear())

  it('merkt sich die Reihenfolge je Schlüssel und setzt zurück', () => {
    const t = useTileOrder(() => 'k:anna', ids)
    expect(t.order.value).toEqual(ids)
    t.set(['d', 'c', 'b', 'a'])
    expect(useTileOrder(() => 'k:anna', ids).order.value).toEqual(['d', 'c', 'b', 'a'])
    expect(useTileOrder(() => 'k:ben', ids).order.value).toEqual(ids)
    t.reset()
    expect(t.order.value).toEqual(ids)
    expect(localStorage.getItem('k:anna')).toBeNull()
  })

  it('übersteht kaputte gespeicherte Daten', () => {
    localStorage.setItem('k:x', '{nicht json')
    expect(useTileOrder(() => 'k:x', ids).order.value).toEqual(ids)
  })
})
