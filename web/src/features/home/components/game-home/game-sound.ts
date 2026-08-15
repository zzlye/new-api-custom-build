/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

/**
 * 游戏音效与环境音合成引擎（基于 Web Audio API）
 * 无需外部音频文件，零网络延迟，纯程序化合成二次元 RPG 风格 UI 音效与空灵背景氛围音
 */

class GameSoundEngine {
  private ctx: AudioContext | null = null
  private muted: boolean = false
  private volume: number = 0.5
  private bgmPlaying: boolean = false
  private bgmNodes: { oscs: OscillatorNode[]; gain: GainNode } | null = null
  private bgmTimer: number | null = null

  constructor() {
    // 从本地存储恢复静音和音量偏好
    try {
      const savedMute = localStorage.getItem('newapi_game_sound_muted')
      this.muted = savedMute !== null ? savedMute === 'true' : false
      const savedVol = localStorage.getItem('newapi_game_sound_volume')
      if (savedVol) this.volume = Math.max(0, Math.min(1, parseFloat(savedVol)))
    } catch {
      // 忽略本地存储不可用的情况
    }
  }

  /** 获取或初始化 AudioContext */
  private getContext(): AudioContext | null {
    if (typeof window === 'undefined') return null
    if (!this.ctx) {
      const AudioContextClass =
        window.AudioContext ||
        (window as unknown as { webkitAudioContext: typeof AudioContext })
          .webkitAudioContext
      if (AudioContextClass) {
        this.ctx = new AudioContextClass()
      }
    }
    if (this.ctx && this.ctx.state === 'suspended') {
      this.ctx.resume().catch(() => {})
    }
    return this.ctx
  }

  /** 是否静音 */
  public isMuted(): boolean {
    return this.muted
  }

  /** 设置静音状态 */
  public setMuted(muted: boolean): void {
    this.muted = muted
    try {
      localStorage.setItem('newapi_game_sound_muted', String(muted))
    } catch {}
    if (muted && this.bgmPlaying) {
      this.stopAmbientBgm()
    }
  }

  /** 获取音量 (0 - 1) */
  public getVolume(): number {
    return this.volume
  }

  /** 设置音量 (0 - 1) */
  public setVolume(vol: number): void {
    this.volume = Math.max(0, Math.min(1, vol))
    try {
      localStorage.setItem('newapi_game_sound_volume', String(this.volume))
    } catch {}
    if (this.bgmNodes) {
      this.bgmNodes.gain.gain.setValueAtTime(
        this.volume * 0.15,
        this.ctx?.currentTime || 0
      )
    }
  }

  /**
   * 播放菜单悬浮音效（清脆空灵的二次元风水晶泛音）
   */
  public playHoverSound(): void {
    if (this.muted) return
    const ctx = this.getContext()
    if (!ctx) return

    const now = ctx.currentTime
    const osc = ctx.createOscillator()
    const osc2 = ctx.createOscillator()
    const gain = ctx.createGain()
    const filter = ctx.createBiquadFilter()

    filter.type = 'highpass'
    filter.frequency.setValueAtTime(800, now)

    // 音高：清脆的 1320Hz（E6）瞬时滑音至 1760Hz（A6）
    osc.type = 'sine'
    osc.frequency.setValueAtTime(1320, now)
    osc.frequency.exponentialRampToValueAtTime(1760, now + 0.08)

    // 泛音叠层增加水晶通透感
    osc2.type = 'triangle'
    osc2.frequency.setValueAtTime(2640, now)
    osc2.frequency.exponentialRampToValueAtTime(3520, now + 0.08)

    // 音量包络：快速启动与指数衰减
    const targetVol = this.volume * 0.12
    gain.gain.setValueAtTime(0.001, now)
    gain.gain.linearRampToValueAtTime(targetVol, now + 0.015)
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.12)

    osc.connect(filter)
    osc2.connect(filter)
    filter.connect(gain)
    gain.connect(ctx.destination)

    osc.start(now)
    osc2.start(now)
    osc.stop(now + 0.13)
    osc2.stop(now + 0.13)
  }

  /**
   * 播放菜单点击/确认音效（刀剑轻鸣与二次元 RPG 确认铃声）
   */
  public playClickSound(): void {
    if (this.muted) return
    const ctx = this.getContext()
    if (!ctx) return

    const now = ctx.currentTime
    const osc1 = ctx.createOscillator()
    const osc2 = ctx.createOscillator()
    const gain = ctx.createGain()

    osc1.type = 'sine'
    osc1.frequency.setValueAtTime(587.33, now) // D5
    osc1.frequency.setValueAtTime(880, now + 0.05) // A5
    osc1.frequency.exponentialRampToValueAtTime(1174.66, now + 0.18) // D6

    osc2.type = 'triangle'
    osc2.frequency.setValueAtTime(1760, now)
    osc2.frequency.exponentialRampToValueAtTime(880, now + 0.18)

    const targetVol = this.volume * 0.2
    gain.gain.setValueAtTime(0.001, now)
    gain.gain.linearRampToValueAtTime(targetVol, now + 0.02)
    gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.22)

    osc1.connect(gain)
    osc2.connect(gain)
    gain.connect(ctx.destination)

    osc1.start(now)
    osc2.start(now)
    osc1.stop(now + 0.23)
    osc2.stop(now + 0.23)
  }

  /**
   * 播放场景/主题切换音效（空灵竖琴升音琶音）
   */
  public playSwitchSound(): void {
    if (this.muted) return
    const ctx = this.getContext()
    if (!ctx) return

    const now = ctx.currentTime
    const notes = [523.25, 659.25, 783.99, 1046.5, 1318.51] // C5, E5, G5, C6, E6

    notes.forEach((freq, idx) => {
      const startTime = now + idx * 0.04
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()

      osc.type = 'sine'
      osc.frequency.setValueAtTime(freq, startTime)

      const targetVol = this.volume * 0.14
      gain.gain.setValueAtTime(0.0001, startTime)
      gain.gain.linearRampToValueAtTime(targetVol, startTime + 0.02)
      gain.gain.exponentialRampToValueAtTime(0.0001, startTime + 0.35)

      osc.connect(gain)
      gain.connect(ctx.destination)

      osc.start(startTime)
      osc.stop(startTime + 0.36)
    })
  }

  /**
   * 启动环境治愈音（空灵夜樱境五声音阶生成器）
   */
  public toggleAmbientBgm(): boolean {
    if (this.bgmPlaying) {
      this.stopAmbientBgm()
      return false
    } else {
      this.startAmbientBgm()
      return true
    }
  }

  public isBgmPlaying(): boolean {
    return this.bgmPlaying
  }

  public startAmbientBgm(): void {
    if (this.muted || this.bgmPlaying) return
    const ctx = this.getContext()
    if (!ctx) return

    this.bgmPlaying = true
    const scale = [261.63, 293.66, 329.63, 392.0, 440.0, 523.25, 587.33, 659.25] // 五声音阶

    const playRandomChord = () => {
      if (!this.bgmPlaying || !this.ctx) return
      const now = this.ctx.currentTime
      const rootIdx = Math.floor(Math.random() * (scale.length - 2))
      const chord = [scale[rootIdx], scale[rootIdx + 2], scale[rootIdx + 4] || scale[rootIdx + 1]]

      chord.forEach((freq, idx) => {
        if (!this.ctx) return
        const osc = this.ctx.createOscillator()
        const gain = this.ctx.createGain()
        const pan = this.ctx.createStereoPanner ? this.ctx.createStereoPanner() : null

        osc.type = idx % 2 === 0 ? 'sine' : 'triangle'
        osc.frequency.setValueAtTime(freq, now)

        if (pan) {
          pan.pan.setValueAtTime((Math.random() - 0.5) * 0.8, now)
        }

        const vol = this.volume * 0.035
        gain.gain.setValueAtTime(0.0001, now)
        gain.gain.linearRampToValueAtTime(vol, now + 1.5)
        gain.gain.exponentialRampToValueAtTime(0.0001, now + 5.5)

        if (pan) {
          osc.connect(pan)
          pan.connect(gain)
        } else {
          osc.connect(gain)
        }
        gain.connect(this.ctx.destination)

        osc.start(now)
        osc.stop(now + 6.0)
      })

      // 每 3.5-5.5 秒随机生成下一个和弦流
      const nextDelay = 3500 + Math.random() * 2000
      this.bgmTimer = window.setTimeout(playRandomChord, nextDelay)
    }

    playRandomChord()
  }

  public stopAmbientBgm(): void {
    this.bgmPlaying = false
    if (this.bgmTimer) {
      window.clearTimeout(this.bgmTimer)
      this.bgmTimer = null
    }
  }
}

export const gameSound = new GameSoundEngine()
