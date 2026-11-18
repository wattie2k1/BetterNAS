import { describe, expect, it } from 'vitest'
import { maskToPrefix, prefixToMask, splitCidr } from '@/utils/netmask'

describe('netmask', () => {
  it('rechnet Präfix in Netzmaske um', () => {
    expect(prefixToMask(24)).toBe('255.255.255.0')
    expect(prefixToMask(16)).toBe('255.255.0.0')
    expect(prefixToMask(25)).toBe('255.255.255.128')
    expect(prefixToMask(0)).toBe('0.0.0.0')
    expect(prefixToMask(32)).toBe('255.255.255.255')
  })
  it('rechnet Netzmaske in Präfix um und lehnt Ungültiges ab', () => {
    expect(maskToPrefix('255.255.255.0')).toBe(24)
    expect(maskToPrefix('255.255.254.0')).toBe(23)
    expect(maskToPrefix('24')).toBe(24)
    expect(maskToPrefix('/16')).toBe(16)
    expect(maskToPrefix('255.0.255.0')).toBeNull()
    expect(maskToPrefix('255.255.255')).toBeNull()
    expect(maskToPrefix('256.0.0.0')).toBeNull()
    expect(maskToPrefix('33')).toBeNull()
    expect(maskToPrefix('')).toBeNull()
  })
  it('trennt CIDR in IP und Maske', () => {
    expect(splitCidr('192.168.1.10/22')).toEqual({ ip: '192.168.1.10', mask: '255.255.252.0' })
    expect(splitCidr('')).toEqual({ ip: '', mask: '255.255.255.0' })
  })
})
