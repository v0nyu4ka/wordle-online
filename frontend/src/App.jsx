import { useState, useEffect, useRef, useCallback } from 'react'

const WS_URL = `ws://${window.location.host}/ws`

export default function App() {
  const [screen, setScreen] = useState('menu')
  const [nickname, setNickname] = useState('')
  const [sessionId, setSessionId] = useState('')
  const [joinId, setJoinId] = useState('')
  const [ws, setWs] = useState(null)
  const [allGuesses, setAllGuesses] = useState([])
  const [myGuesses, setMyGuesses] = useState([])
  const [currentInput, setCurrentInput] = useState('')
  const [solved, setSolved] = useState(false)
  const [answer, setAnswer] = useState('')
  const [toast, setToast] = useState('')
  const [players, setPlayers] = useState({})
  const [gameOver, setGameOver] = useState(false)
  const [copied, setCopied] = useState(false)
  const [feedOpen, setFeedOpen] = useState(false)
  const feedRef = useRef(null)

  const showToast = (msg, ms = 2500) => { setToast(msg); setTimeout(() => setToast(''), ms) }

  const connect = useCallback(() => {
    const socket = new WebSocket(WS_URL)
    socket.onopen = () => setWs(socket)
    socket.onmessage = (e) => {
      const msg = JSON.parse(e.data)
      switch (msg.type) {
        case 'created':
          setSessionId(msg.data.sessionId)
          setScreen('game')
          break
        case 'joined':
          setSessionId(msg.data.sessionId)
          setAllGuesses(msg.data.guesses || [])
          setPlayers(msg.data.players || {})
          setScreen('game')
          break
        case 'guess_result':
          setMyGuesses(prev => [...prev, msg.data.guess])
          setAllGuesses(prev => [...prev, msg.data.guess])
          if (msg.data.solved) { setSolved(true); setAnswer(msg.data.answer) }
          if (msg.data.answer && !msg.data.solved) { setGameOver(true); setAnswer(msg.data.answer) }
          break
        case 'player_guess':
          setAllGuesses(prev => [...prev, msg.data])
          break
        case 'player_joined':
          showToast(`${msg.data.nickname} joined!`)
          setPlayers(prev => ({ ...prev, [msg.data.nickname]: { nickname: msg.data.nickname, guesses: [], solved: false } }))
          break
        case 'player_left':
          showToast(`${msg.data.nickname} left`)
          break
        case 'new_round':
          setMyGuesses([]); setAllGuesses([]); setSolved(false); setAnswer(''); setGameOver(false); setCurrentInput('')
          showToast('New word! Go!')
          break
        case 'error':
          showToast(msg.error)
          break
      }
    }
    socket.onclose = () => setTimeout(connect, 2000)
    return socket
  }, [])

  useEffect(() => { const s = connect(); return () => s.close() }, [])

  useEffect(() => {
    const handler = (e) => { if (e.ctrlKey && e.key === 'c') e.preventDefault() }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  const createSession = () => { if (ws && nickname.trim()) ws.send(JSON.stringify({ type: 'create', nickname: nickname.trim() })) }
  const joinSession = () => { if (ws && nickname.trim() && joinId.trim()) ws.send(JSON.stringify({ type: 'join', nickname: nickname.trim(), session: joinId.trim() })) }
  const submitGuess = () => {
    if (!ws || currentInput.length !== 5 || solved || gameOver) return
    ws.send(JSON.stringify({ type: 'guess', word: currentInput.toLowerCase() }))
    setCurrentInput('')
  }
  const newWord = () => ws?.send(JSON.stringify({ type: 'new_word' }))
  const copySessionId = () => { navigator.clipboard.writeText(sessionId); setCopied(true); setTimeout(() => setCopied(false), 2000) }

  const handleKey = useCallback((e) => {
    if (screen !== 'game' || solved || gameOver) return
    if (e.ctrlKey) return
    if (e.key === 'Enter') { submitGuess(); return }
    if (e.key === 'Backspace') { setCurrentInput(prev => prev.slice(0, -1)); return }
    if (/^[a-zA-Z]$/.test(e.key) && currentInput.length < 5) setCurrentInput(prev => prev + e.key.toLowerCase())
  }, [screen, currentInput, solved, gameOver, ws])

  useEffect(() => { window.addEventListener('keydown', handleKey); return () => window.removeEventListener('keydown', handleKey) }, [handleKey])

  // Group guesses by player
  const guessesByPlayer = {}
  allGuesses.forEach(g => {
    if (!guessesByPlayer[g.player]) guessesByPlayer[g.player] = []
    guessesByPlayer[g.player].push(g)
  })

  const renderGrid = (guesses, current = '') => {
    const rows = []
    for (let i = 0; i < 6; i++) {
      const guess = guesses[i]
      const cells = []
      for (let j = 0; j < 5; j++) {
        if (guess) {
          const r = guess.results[j]
          cells.push(<div key={j} className={`cell ${r.status}`}>{r.letter}</div>)
        } else if (i === guesses.length) {
          cells.push(<div key={j} className={`cell ${j < current.length ? 'filled' : ''}`}>{current[j] || ''}</div>)
        } else {
          cells.push(<div key={j} className="cell" />)
        }
      }
      rows.push(<div key={i} className="row">{cells}</div>)
    }
    return <div className="grid">{rows}</div>
  }

  const keyboard = [
    ['q','w','e','r','t','y','u','i','o','p'],
    ['a','s','d','f','g','h','j','k','l'],
    ['Enter','z','x','c','v','b','n','m','⌫']
  ]

  const getKeyStatus = (key) => {
    let status = ''
    for (const g of myGuesses) {
      for (const r of g.results) {
        if (r.letter === key) {
          if (r.status === 'correct') return 'correct'
          if (r.status === 'present' && status !== 'correct') status = 'present'
          if (r.status === 'absent' && !status) status = 'absent'
        }
      }
    }
    return status
  }

  const playerCount = Object.keys(guessesByPlayer).length

  if (screen === 'menu') {
    return (
      <div className="app menu">
        <h1>🟩 Wordle</h1>
        <p className="subtitle">Online Multiplayer</p>
        <input placeholder="Your nickname" value={nickname} onChange={e => setNickname(e.target.value)} maxLength={15}
          onKeyDown={e => e.key === 'Enter' && nickname.trim() && !joinId.trim() && createSession()} />
        <button onClick={createSession} disabled={!nickname.trim()}>Create Game</button>
        <div className="divider">or join</div>
        <input placeholder="Room code" value={joinId} onChange={e => setJoinId(e.target.value)} maxLength={6}
          onKeyDown={e => e.key === 'Enter' && joinSession()} />
        <button className="btn-secondary" onClick={joinSession} disabled={!nickname.trim() || !joinId.trim()}>Join Game</button>
      </div>
    )
  }

  return (
    <div className="app game">
      {toast && <div className="toast">{toast}</div>}

      {/* Mobile feed toggle */}
      <button className="feed-toggle" onClick={() => setFeedOpen(!feedOpen)}>
        👥 {playerCount} {feedOpen ? '✕' : ''}
      </button>

      <div className={`feed ${feedOpen ? 'feed-open' : ''}`} ref={feedRef}>
        <div className="feed-header">
          <h3>Players</h3>
          <div className="session-code">
            <span>{sessionId}</span>
            <button className="copy-btn" onClick={copySessionId}>{copied ? '✅' : '📋'}</button>
          </div>
        </div>

        {Object.entries(guessesByPlayer).map(([player, pGuesses]) => (
          <div key={player} className="player-section">
            <div className="player-name">{player} {player === nickname ? '(you)' : ''}</div>
            <div className="player-guesses">
              {pGuesses.map((g, i) => (
                <div key={i} className="feed-row">
                  {g.results.map((r, j) => (
                    <span key={j} className={`feed-cell ${r.status}`} />
                  ))}
                </div>
              ))}
              {pGuesses.length === 0 && <div className="feed-empty-player">...</div>}
            </div>
          </div>
        ))}

        {playerCount === 0 && <div className="feed-empty">Waiting for guesses...</div>}
      </div>

      <div className="main">
        {solved && <div className="result success">🎉 <strong>{answer.toUpperCase()}</strong></div>}
        {gameOver && !solved && <div className="result failure">💀 <strong>{answer.toUpperCase()}</strong></div>}

        {renderGrid(myGuesses, currentInput)}

        <div className="keyboard">
          {keyboard.map((row, i) => (
            <div key={i} className="kb-row">
              {row.map(key => (
                <button key={key} className={`kb-key ${key.length > 1 ? 'wide' : ''} ${getKeyStatus(key)}`}
                  onClick={() => {
                    if (key === 'Enter') submitGuess()
                    else if (key === '⌫') setCurrentInput(prev => prev.slice(0, -1))
                    else if (currentInput.length < 5) setCurrentInput(prev => prev + key)
                  }}>{key}</button>
              ))}
            </div>
          ))}
        </div>

        {(solved || gameOver) && <button className="new-word-btn" onClick={newWord}>🔄 New Word</button>}
      </div>
    </div>
  )
}
