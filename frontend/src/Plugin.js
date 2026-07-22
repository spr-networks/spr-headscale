import React, { useEffect, useState } from 'react'
import {
  api,
  useAlert,
  timeAgo,
  Page,
  ListHeader,
  ListItem,
  Card,
  SectionHeader,
  StatTile,
  KeyVal,
  StatusDot,
  Toggle,
  TextField,
  ModalForm,
  ModalConfirm,
  Loading,
  EmptyState,
  Box,
  Button,
  ButtonText,
  HStack,
  Pressable,
  VStack,
  Text
} from '@spr-networks/plugin-ui'

const BASE = `/plugins/${api.pluginURI() || 'spr-headscale'}`

const dash = (v) => v || '—'

// mirrors the backend's --expiration validator (30m, 24h, 90d, ...)
const rDuration = /^([0-9]+(ms|s|m|h|d|w|y))+$/
const rServerURL = /^https?:\/\/\S+$/
const rBaseDomain =
  /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$/

// countdown counterpart to timeAgo: "3h" for a future RFC3339 stamp, null otherwise
const timeIn = (ts) => {
  if (!ts) return null
  const then = Date.parse(ts)
  if (Number.isNaN(then)) return null
  const secs = Math.floor((then - Date.now()) / 1000)
  if (secs <= 0) return null
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.floor(secs / 60)}m`
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`
  return `${Math.floor(secs / 86400)}d`
}

const joinCommand = (serverURL, key) =>
  `tailscale up --login-server ${serverURL || '<server-url>'} --authkey ${
    key || '<auth-key>'
  }`

// ---- small fittings

const TabRow = ({ tabs, active, onChange }) => (
  <HStack
    space="xs"
    p="$1"
    borderRadius="$xl"
    borderWidth={1}
    borderColor="$borderColorCardLight"
    bg="$backgroundCardLight"
    alignSelf="flex-start"
    flexWrap="wrap"
    sx={{
      _dark: { bg: '$backgroundCardDark', borderColor: '$borderColorCardDark' }
    }}
  >
    {tabs.map((t) => {
      const isActive = t.id === active
      return (
        <Pressable
          key={t.id}
          onPress={() => onChange(t.id)}
          borderRadius="$lg"
          px="$4"
          py="$1.5"
          bg={isActive ? '$primary600' : 'transparent'}
          sx={{ _dark: { bg: isActive ? '$primary500' : 'transparent' } }}
          accessibilityRole="tab"
          accessibilityState={{ selected: isActive }}
        >
          <Text
            size="sm"
            fontWeight={isActive ? '$semibold' : '$normal'}
            color={isActive ? '$white' : '$muted500'}
          >
            {t.label}
          </Text>
        </Pressable>
      )
    })}
  </HStack>
)

const CopyRow = ({ label, value, onCopy }) => (
  <HStack space="md" alignItems="center" flexWrap="wrap" py="$0.5">
    <Text size="sm" color="$muted500" minWidth={132}>
      {label}
    </Text>
    <HStack space="sm" alignItems="center" flexShrink={1} flexWrap="wrap">
      <Text
        size="sm"
        fontFamily="$mono"
        color="$textLight900"
        sx={{ _dark: { color: '$textDark100' } }}
      >
        {dash(value)}
      </Text>
      {value ? (
        <Button size="xs" variant="link" onPress={() => onCopy(value)}>
          <ButtonText>Copy</ButtonText>
        </Button>
      ) : null}
    </HStack>
  </HStack>
)

const CodeBlock = ({ text, onCopy, copyLabel = 'Copy' }) => (
  <VStack space="xs">
    <Box
      p="$3"
      borderRadius="$lg"
      borderWidth={1}
      borderColor="$muted100"
      bg="$backgroundContentLight"
      sx={{
        _dark: {
          bg: '$backgroundContentDark',
          borderColor: '$borderColorCardDark'
        }
      }}
    >
      <Text size="sm" fontFamily="$mono" selectable>
        {text}
      </Text>
    </Box>
    <HStack justifyContent="flex-end">
      <Button size="xs" variant="outline" onPress={() => onCopy(text)}>
        <ButtonText>{copyLabel}</ButtonText>
      </Button>
    </HStack>
  </VStack>
)

const Step = ({ n, title, description, children }) => (
  <HStack space="md" alignItems="flex-start">
    <Box
      w={26}
      h={26}
      mt="$0.5"
      flexShrink={0}
      borderRadius="$full"
      alignItems="center"
      justifyContent="center"
      bg="$primary600"
      sx={{ _dark: { bg: '$primary500' } }}
    >
      <Text size="xs" color="$white" fontWeight="$bold">
        {n}
      </Text>
    </Box>
    <VStack space="xs" flex={1}>
      <Text bold>{title}</Text>
      {description ? (
        <Text size="sm" color="$muted500">
          {description}
        </Text>
      ) : null}
      {children}
    </VStack>
  </HStack>
)

const TABS = [
  { id: 'overview', label: 'Overview' },
  { id: 'users', label: 'Users & keys' },
  { id: 'nodes', label: 'Nodes' },
  { id: 'settings', label: 'Settings' }
]

export default function Plugin() {
  const alert = useAlert()
  const [loading, setLoading] = useState(true)
  const [fetchError, setFetchError] = useState(null)
  const [status, setStatus] = useState(null)
  const [config, setConfig] = useState(null)
  const [users, setUsers] = useState([])
  const [nodes, setNodes] = useState([])
  const [keys, setKeys] = useState([])
  const [tab, setTab] = useState('overview')

  // settings form
  const [serverURL, setServerURL] = useState('')
  const [baseDomain, setBaseDomain] = useState('')
  const [magicDNS, setMagicDNS] = useState(true)
  const [saving, setSaving] = useState(false)

  // users
  const [newUser, setNewUser] = useState('')
  const [addingUser, setAddingUser] = useState(false)
  const [deleteUser, setDeleteUser] = useState(null)

  // preauth keys
  const [keyUser, setKeyUser] = useState(null) // user object for the generator modal
  const [keyReusable, setKeyReusable] = useState(false)
  const [keyEphemeral, setKeyEphemeral] = useState(false)
  const [keyExpiration, setKeyExpiration] = useState('1h')
  const [generatingKey, setGeneratingKey] = useState(false)
  const [createdKey, setCreatedKey] = useState(null) // copy-once modal

  // nodes
  const [deleteNode, setDeleteNode] = useState(null)
  const [expireNode, setExpireNode] = useState(null)

  // daemon
  const [showRestart, setShowRestart] = useState(false)
  const [restarting, setRestarting] = useState(false)

  const refresh = () => {
    Promise.allSettled([
      api.get(`${BASE}/status`),
      api.get(`${BASE}/config`),
      api.get(`${BASE}/users`),
      api.get(`${BASE}/nodes`),
      api.get(`${BASE}/preauthkeys`)
    ]).then(([s, c, u, n, k]) => {
      if (s.status === 'fulfilled') {
        setStatus(s.value)
        setFetchError(null)
      } else {
        setFetchError(s.reason?.message || 'backend unreachable')
      }
      if (c.status === 'fulfilled' && c.value) {
        setConfig(c.value)
        setServerURL(c.value.ServerURL || '')
        setBaseDomain(c.value.BaseDomain || '')
        setMagicDNS(!!c.value.MagicDNS)
      }
      if (u.status === 'fulfilled') setUsers(u.value || [])
      if (n.status === 'fulfilled') setNodes(n.value || [])
      if (k.status === 'fulfilled') setKeys(k.value || [])
      setLoading(false)
    })
  }

  useEffect(() => {
    refresh()
    const statusTimer = setInterval(() => {
      api
        .get(`${BASE}/status`)
        .then((value) => {
          setStatus(value)
          setFetchError(null)
        })
        .catch((err) => setFetchError(err?.message || 'backend unreachable'))
    }, 5000)
    return () => clearInterval(statusTimer)
  }, [])

  const copy = (text, message = 'Copied to clipboard') => {
    if (navigator?.clipboard?.writeText) {
      navigator.clipboard
        .writeText(text)
        .then(() => alert.success(message))
        .catch(() => alert.warning('Copy failed — select and copy manually'))
    } else {
      alert.warning('Clipboard unavailable — select and copy manually')
    }
  }

  // ---- settings

  const serverURLError =
    serverURL.trim() && !rServerURL.test(serverURL.trim())
      ? 'Must be an http:// or https:// URL'
      : null
  const baseDomainError =
    baseDomain.trim() && !rBaseDomain.test(baseDomain.trim())
      ? 'Must be a lowercase domain like headscale.internal'
      : null
  const settingsDirty =
    !!config &&
    (serverURL.trim() !== (config.ServerURL || '') ||
      baseDomain.trim() !== (config.BaseDomain || '') ||
      magicDNS !== !!config.MagicDNS)

  const saveConfig = () => {
    setSaving(true)
    api
      .put(`${BASE}/config`, {
        ServerURL: serverURL.trim(),
        BaseDomain: baseDomain.trim(),
        MagicDNS: magicDNS,
        DERPEnabled: true
      })
      .then(() => {
        alert.success('Settings applied — headscale restarted')
        refresh()
      })
      .catch((err) => {
        alert.error('Failed to save settings', err)
        setTab('overview')
        refresh()
      })
      .finally(() => setSaving(false))
  }

  const restart = () => {
    setRestarting(true)
    api
      .post(`${BASE}/restart`)
      .then(() => {
        alert.success('headscale restarted')
        refresh()
      })
      .catch((err) => {
        alert.error('Restart failed', err)
        setTab('overview')
        refresh()
      })
      .finally(() => setRestarting(false))
  }

  // ---- users

  const addUser = () => {
    let name = newUser.trim()
    if (!name) {
      return alert.warning('Enter a user name')
    }
    setAddingUser(true)
    api
      .post(`${BASE}/users`, { Name: name })
      .then(() => {
        alert.success(`User ${name} created`)
        setNewUser('')
        refresh()
      })
      .catch((err) => alert.error('Failed to create user', err))
      .finally(() => setAddingUser(false))
  }

  const doDeleteUser = () => {
    let u = deleteUser
    setDeleteUser(null)
    api
      .delete(`${BASE}/users/${u.ID}`)
      .then(() => {
        alert.success(`User ${u.Name} deleted`)
        refresh()
      })
      .catch((err) => alert.error('Failed to delete user', err))
  }

  // ---- preauth keys

  const keyExpirationError =
    keyExpiration.trim() && !rDuration.test(keyExpiration.trim())
      ? 'Use a duration like 30m, 24h or 90d'
      : null

  const openKeyModal = (u) => {
    setKeyReusable(false)
    setKeyEphemeral(false)
    setKeyExpiration('1h')
    setKeyUser(u)
  }

  const createKey = () => {
    let u = keyUser
    setGeneratingKey(true)
    api
      .post(`${BASE}/preauthkeys`, {
        User: u.ID,
        Reusable: keyReusable,
        Ephemeral: keyEphemeral,
        Expiration: keyExpiration.trim() || '1h'
      })
      .then((k) => {
        setKeyUser(null)
        setCreatedKey({ ...k, UserName: u.Name })
        refresh()
      })
      .catch((err) => alert.error('Failed to generate key', err))
      .finally(() => setGeneratingKey(false))
  }

  // ---- nodes

  const doExpireNode = () => {
    let n = expireNode
    setExpireNode(null)
    api
      .post(`${BASE}/nodes/${n.ID}/expire`)
      .then(() => {
        alert.success(`Key expired for ${n.GivenName || n.Name}`)
        refresh()
      })
      .catch((err) => alert.error('Failed to expire node', err))
  }

  const doDeleteNode = () => {
    let n = deleteNode
    setDeleteNode(null)
    api
      .delete(`${BASE}/nodes/${n.ID}`)
      .then(() => {
        alert.success(`Node ${n.GivenName || n.Name} removed`)
        refresh()
      })
      .catch((err) => alert.error('Failed to remove node', err))
  }

  // ---- derived

  const onlineCount = nodes.filter((n) => n.Online).length
  const nodeCountFor = (u) => nodes.filter((n) => n.UserID === u.ID).length
  const firstRun = !fetchError && users.length === 0 && nodes.length === 0
  const running = !!status?.Running

  if (loading) {
    return (
      <Page>
        <Loading />
      </Page>
    )
  }

  const header = (
    <ListHeader
      title="Headscale"
      description="Self-hosted Tailscale control server"
      mark="hs"
      status={fetchError ? 'Unreachable' : running ? 'Running' : 'Stopped'}
      statusAction={
        fetchError
          ? 'error'
          : running
          ? 'success'
          : status?.LastError
          ? 'error'
          : 'muted'
      }
    >
      <Button
        size="sm"
        variant="outline"
        isDisabled={restarting}
        onPress={() => setShowRestart(true)}
      >
        <ButtonText>{restarting ? 'Restarting…' : 'Restart'}</ButtonText>
      </Button>
    </ListHeader>
  )

  if (fetchError && !status) {
    return (
      <Page>
        {header}
        <Card>
          <EmptyState
            title="Backend unreachable"
            description={`The spr-headscale plugin API did not respond (${fetchError}). Check that the plugin container is running, then retry.`}
          >
            <Button
              size="sm"
              onPress={() => {
                setLoading(true)
                refresh()
              }}
            >
              <ButtonText>Retry</ButtonText>
            </Button>
          </EmptyState>
        </Card>
      </Page>
    )
  }

  const stoppedNotice = !running ? (
    <Card tone="warning" p="$4">
      <HStack
        justifyContent="space-between"
        alignItems="center"
        flexWrap="wrap"
        gap="$2"
      >
        <VStack flexShrink={1}>
          <Text bold>headscale is stopped</Text>
          <Text size="sm" color="$muted500">
            {status?.LastError ||
              "Users, keys and nodes can't be managed until the daemon runs."}
          </Text>
        </VStack>
        <Button
          size="sm"
          variant="outline"
          isDisabled={restarting}
          onPress={() => setShowRestart(true)}
        >
          <ButtonText>{restarting ? 'Restarting…' : 'Restart'}</ButtonText>
        </Button>
      </HStack>
    </Card>
  ) : null

  // ---- tabs

  const daemonDiagnostics = status?.RecentLogs ? (
    <Card tone={status?.LastError ? 'warning' : undefined}>
      <SectionHeader title="Headscale logs" />
      {status?.LastError ? (
        <Text size="sm" color="$error600" mb="$3" selectable>
          {status.LastError}
        </Text>
      ) : null}
      <CodeBlock
        text={status.RecentLogs}
        onCopy={(value) => copy(value, 'Headscale logs copied')}
        copyLabel="Copy logs"
      />
    </Card>
  ) : null

  const overviewTab = (
    <>
      <Card>
        <SectionHeader
          title="Control server"
          right={<StatusDot online={running} />}
        />
        <HStack flexWrap="wrap" gap="$2">
          <StatTile label="State" value={running ? 'Running' : 'Stopped'} />
          <StatTile
            label="Version"
            value={status?.Version || status?.PinnedVersion || '—'}
            mono
          />
          <StatTile label="Users" value={String(users.length)} />
          <StatTile label="Nodes" value={String(nodes.length)} />
          <StatTile label="Online now" value={String(onlineCount)} />
        </HStack>
        <VStack space="xs" mt="$3">
          <CopyRow
            label="Server URL"
            value={status?.ServerURL}
            onCopy={(v) => copy(v, 'Server URL copied')}
          />
          <KeyVal label="Listening on" value={dash(status?.ListenAddr)} mono />
          <KeyVal
            label="MagicDNS"
            value={status?.MagicDNS ? `on (${status?.BaseDomain})` : 'off'}
          />
          <KeyVal
            label="DERP relays"
            value={
              status?.DERPEnabled
                ? 'Tailscale public map'
                : 'off — direct connections only'
            }
          />
        </VStack>
      </Card>

      {daemonDiagnostics}

      {firstRun ? (
        <Card>
          <SectionHeader title="Set up your tailnet" />
          <VStack space="lg">
            <Step
              n="1"
              title="Set the server URL (optional)"
              description="Devices dial this URL to coordinate. The default container address works for devices on your LAN; set a public https:// URL for roaming devices."
            >
              <HStack>
                <Button
                  size="xs"
                  variant="outline"
                  onPress={() => setTab('settings')}
                >
                  <ButtonText>Open settings</ButtonText>
                </Button>
              </HStack>
            </Step>
            <Step
              n="2"
              title="Create a user"
              description="Users own devices — one per person is typical."
            >
              <HStack>
                <Button
                  size="xs"
                  variant="outline"
                  onPress={() => setTab('users')}
                >
                  <ButtonText>Create user</ButtonText>
                </Button>
              </HStack>
            </Step>
            <Step
              n="3"
              title="Generate a preauth key"
              description="Use “Generate key” next to the user. The key is shown once, with the join command ready to copy."
            />
            <Step
              n="4"
              title="Join a device"
              description="On the device, run the join command with your key:"
            >
              <CodeBlock
                text={joinCommand(status?.ServerURL)}
                onCopy={(t) => copy(t, 'Join command copied')}
                copyLabel="Copy command"
              />
            </Step>
          </VStack>
        </Card>
      ) : (
        <Card>
          <SectionHeader title="Join a device" />
          <VStack space="sm">
            <Text size="sm" color="$muted500">
              Run this on a device with Tailscale installed, replacing
              &lt;auth-key&gt; with a preauth key. Generating a key shows the
              complete command ready to paste.
            </Text>
            <CodeBlock
              text={joinCommand(status?.ServerURL)}
              onCopy={(t) => copy(t, 'Join command copied')}
              copyLabel="Copy command"
            />
          </VStack>
        </Card>
      )}
    </>
  )

  const usersTab = (
    <>
      <Card>
        <SectionHeader title="Users" count={users.length} />
        <VStack space="sm">
          {users.length === 0 ? (
            <EmptyState
              title="No users yet"
              description="Users own devices. Create one, then generate a preauth key to join devices under it."
            />
          ) : (
            users.map((u) => (
              <ListItem key={u.ID}>
                <HStack
                  flex={1}
                  justifyContent="space-between"
                  alignItems="center"
                  flexWrap="wrap"
                  gap="$2"
                >
                  <VStack>
                    <Text bold>{u.Name}</Text>
                    <Text size="xs" color="$muted500">
                      {nodeCountFor(u)}{' '}
                      {nodeCountFor(u) === 1 ? 'node' : 'nodes'}
                      {' · '}id {u.ID}
                      {u.CreatedAt
                        ? ` · created ${timeAgo(u.CreatedAt) || '—'}`
                        : ''}
                    </Text>
                  </VStack>
                  <HStack space="sm">
                    <Button
                      size="xs"
                      variant="outline"
                      onPress={() => openKeyModal(u)}
                    >
                      <ButtonText>Generate key</ButtonText>
                    </Button>
                    <Button
                      size="xs"
                      variant="outline"
                      action="negative"
                      onPress={() => setDeleteUser(u)}
                    >
                      <ButtonText>Delete</ButtonText>
                    </Button>
                  </HStack>
                </HStack>
              </ListItem>
            ))
          )}
          <HStack space="sm" alignItems="flex-end">
            <VStack flex={1}>
              <TextField
                label="New user"
                value={newUser}
                onChangeText={setNewUser}
                placeholder="alice"
              />
            </VStack>
            <Button size="sm" isDisabled={addingUser} onPress={addUser}>
              <ButtonText>{addingUser ? 'Creating…' : 'Create user'}</ButtonText>
            </Button>
          </HStack>
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="Preauth keys" count={keys.length} />
        {keys.length === 0 ? (
          <EmptyState
            title="No preauth keys"
            description="A preauth key lets a device join without interactive login. Generate one from a user above — the full key is shown exactly once."
          />
        ) : (
          <VStack space="sm">
            {keys.map((k) => (
              <ListItem key={k.ID}>
                <HStack
                  flex={1}
                  justifyContent="space-between"
                  alignItems="center"
                  flexWrap="wrap"
                  gap="$2"
                >
                  <VStack>
                    <Text fontFamily="$mono">{k.KeyPrefix}</Text>
                    <Text size="xs" color="$muted500">
                      {k.UserName}
                      {k.Reusable ? ' · reusable' : ' · single-use'}
                      {k.Ephemeral ? ' · ephemeral' : ''}
                      {k.Used ? ' · used' : ''}
                      {k.CreatedAt
                        ? ` · created ${timeAgo(k.CreatedAt) || '—'}`
                        : ''}
                    </Text>
                  </VStack>
                  <Text size="xs" color={k.Expired ? '$red500' : '$muted500'}>
                    {k.Expired
                      ? `expired ${timeAgo(k.Expires) || ''}`.trim()
                      : timeIn(k.Expires)
                        ? `expires in ${timeIn(k.Expires)}`
                        : ''}
                  </Text>
                </HStack>
              </ListItem>
            ))}
          </VStack>
        )}
      </Card>
    </>
  )

  const nodesTab = (
    <Card>
      <SectionHeader
        title="Nodes"
        count={nodes.length}
        right={
          <Text size="xs" color="$muted500">
            {onlineCount} online
          </Text>
        }
      />
      {nodes.length === 0 ? (
        <EmptyState
          title="No nodes"
          description="Devices appear here once they join your tailnet with a preauth key."
        >
          <Button size="sm" onPress={() => setTab('users')}>
            <ButtonText>Generate a key</ButtonText>
          </Button>
        </EmptyState>
      ) : (
        <VStack space="sm">
          {nodes.map((n) => (
            <ListItem key={n.ID}>
              <HStack
                flex={1}
                justifyContent="space-between"
                alignItems="center"
                flexWrap="wrap"
                gap="$2"
              >
                <HStack space="md" alignItems="center">
                  <StatusDot online={n.Online} warn={n.Expired} />
                  <VStack>
                    <Text bold>{n.GivenName || n.Name}</Text>
                    <Text size="xs" color="$muted500" fontFamily="$mono">
                      {(n.IPs || []).join(' · ') || '—'}
                    </Text>
                    <Text size="xs" color="$muted500">
                      {n.User}
                      {' · '}
                      {n.Online
                        ? 'online'
                        : `last seen ${timeAgo(n.LastSeen) || 'never'}`}
                      {n.Expired ? ' · key expired' : ''}
                    </Text>
                  </VStack>
                </HStack>
                <HStack space="sm">
                  <Button
                    size="xs"
                    variant="outline"
                    onPress={() => setExpireNode(n)}
                  >
                    <ButtonText>Expire key</ButtonText>
                  </Button>
                  <Button
                    size="xs"
                    variant="outline"
                    action="negative"
                    onPress={() => setDeleteNode(n)}
                  >
                    <ButtonText>Remove</ButtonText>
                  </Button>
                </HStack>
              </HStack>
            </ListItem>
          ))}
        </VStack>
      )}
    </Card>
  )

  const settingsTab = (
    <Card>
      <SectionHeader title="Settings" />
      <VStack space="md">
        <TextField
          label="Server URL"
          value={serverURL}
          onChangeText={setServerURL}
          placeholder={`http://${status?.ContainerIP || 'container-ip'}:8080`}
          error={serverURLError}
          helper="URL tailscale clients use to reach headscale. Leave empty to use the container IP (LAN-only). For public access set your reverse-proxied https:// URL."
        />
        <TextField
          label="MagicDNS base domain"
          value={baseDomain}
          onChangeText={setBaseDomain}
          placeholder="headscale.internal"
          error={baseDomainError}
          helper="Node hostnames become <name>.<base domain>. Must differ from the Server URL host."
        />
        <HStack justifyContent="space-between" alignItems="center">
          <VStack flex={1} pr="$4">
            <Text>MagicDNS</Text>
            <Text size="xs" color="$muted500">
              Resolve node names inside the tailnet.
            </Text>
          </VStack>
          <Toggle
            value={magicDNS}
            label="MagicDNS"
            onPress={() => setMagicDNS(!magicDNS)}
          />
        </HStack>
        <HStack justifyContent="space-between" alignItems="center">
          <VStack flex={1} pr="$4">
            <Text>DERP relays (required)</Text>
            <Text size="xs" color="$muted500">
              Headscale requires a non-empty initial DERP map. Tailscale's
              public relay map is used for NAT traversal and fallback.
            </Text>
          </VStack>
          <Text size="sm">On</Text>
        </HStack>
        <HStack
          justifyContent="space-between"
          alignItems="center"
          flexWrap="wrap"
          gap="$2"
        >
          <Text size="xs" color="$muted500" flexShrink={1}>
            Applying restarts headscale; devices reconnect automatically.
          </Text>
          <Button
            size="sm"
            isDisabled={
              !settingsDirty || saving || !!serverURLError || !!baseDomainError
            }
            onPress={saveConfig}
          >
            <ButtonText>{saving ? 'Applying…' : 'Save & apply'}</ButtonText>
          </Button>
        </HStack>
      </VStack>
    </Card>
  )

  return (
    <Page>
      {header}
      {stoppedNotice}
      <TabRow tabs={TABS} active={tab} onChange={setTab} />
      {tab === 'overview' ? overviewTab : null}
      {tab === 'users' ? usersTab : null}
      {tab === 'nodes' ? nodesTab : null}
      {tab === 'settings' ? settingsTab : null}

      {/* per-user preauth key generator */}
      <ModalForm
        isOpen={!!keyUser}
        onClose={() => setKeyUser(null)}
        title={`Generate key for ${keyUser?.Name || ''}`}
      >
        <VStack space="md">
          <TextField
            label="Expires after"
            value={keyExpiration}
            onChangeText={setKeyExpiration}
            placeholder="1h"
            error={keyExpirationError}
            helper="Duration like 30m, 24h, 90d"
          />
          <HStack justifyContent="space-between" alignItems="center">
            <VStack flex={1} pr="$4">
              <Text>Reusable</Text>
              <Text size="xs" color="$muted500">
                Join multiple devices with the same key.
              </Text>
            </VStack>
            <Toggle
              value={keyReusable}
              label="Reusable"
              onPress={() => setKeyReusable(!keyReusable)}
            />
          </HStack>
          <HStack justifyContent="space-between" alignItems="center">
            <VStack flex={1} pr="$4">
              <Text>Ephemeral</Text>
              <Text size="xs" color="$muted500">
                Devices are removed automatically shortly after they go
                offline.
              </Text>
            </VStack>
            <Toggle
              value={keyEphemeral}
              label="Ephemeral"
              onPress={() => setKeyEphemeral(!keyEphemeral)}
            />
          </HStack>
          <Button
            size="sm"
            isDisabled={generatingKey || !!keyExpirationError}
            onPress={createKey}
          >
            <ButtonText>
              {generatingKey ? 'Generating…' : 'Generate key'}
            </ButtonText>
          </Button>
        </VStack>
      </ModalForm>

      {/* copy-once key display */}
      <ModalForm
        isOpen={!!createdKey}
        onClose={() => setCreatedKey(null)}
        title="Preauth key created"
      >
        <VStack space="md">
          <Text size="sm">
            Copy this key now — it will not be shown again. Only a masked
            prefix is listed afterwards.
          </Text>
          <CodeBlock
            text={createdKey?.Key || ''}
            onCopy={(t) => copy(t, 'Key copied')}
            copyLabel="Copy key"
          />
          <Text size="xs" color="$muted500">
            {createdKey?.UserName}
            {createdKey?.Reusable ? ' · reusable' : ' · single-use'}
            {createdKey?.Ephemeral ? ' · ephemeral' : ''}
            {createdKey?.Expires && timeIn(createdKey.Expires)
              ? ` · expires in ${timeIn(createdKey.Expires)}`
              : ''}
          </Text>
          <VStack space="xs">
            <Text size="sm" bold>
              Join command
            </Text>
            <Text size="xs" color="$muted500">
              Run on the device to add it to the tailnet:
            </Text>
            <CodeBlock
              text={joinCommand(status?.ServerURL, createdKey?.Key)}
              onCopy={(t) => copy(t, 'Join command copied')}
              copyLabel="Copy command"
            />
          </VStack>
          <HStack justifyContent="flex-end">
            <Button size="sm" onPress={() => setCreatedKey(null)}>
              <ButtonText>I've saved this key</ButtonText>
            </Button>
          </HStack>
        </VStack>
      </ModalForm>

      <ModalConfirm
        isOpen={!!deleteUser}
        onClose={() => setDeleteUser(null)}
        onConfirm={doDeleteUser}
        title={`Delete user ${deleteUser?.Name}?`}
        message={
          deleteUser && nodeCountFor(deleteUser) > 0
            ? `${deleteUser.Name} still owns ${nodeCountFor(deleteUser)} node(s). headscale refuses to delete a user with registered nodes — remove those nodes first.`
            : `This destroys ${deleteUser?.Name || 'the user'} in headscale. Their preauth keys stop working immediately.`
        }
        confirmText="Delete user"
        destructive
      />

      <ModalConfirm
        isOpen={!!expireNode}
        onClose={() => setExpireNode(null)}
        onConfirm={doExpireNode}
        title={`Expire key for ${expireNode?.GivenName || expireNode?.Name}?`}
        message="The node keeps its registration but is logged out and must re-authenticate with a new preauth key before it can reconnect."
        confirmText="Expire key"
      />

      <ModalConfirm
        isOpen={!!deleteNode}
        onClose={() => setDeleteNode(null)}
        onConfirm={doDeleteNode}
        title={`Remove node ${deleteNode?.GivenName || deleteNode?.Name}?`}
        message="The device is disconnected from the tailnet immediately and needs a new preauth key to rejoin."
        confirmText="Remove node"
        destructive
      />

      <ModalConfirm
        isOpen={showRestart}
        onClose={() => setShowRestart(false)}
        onConfirm={restart}
        title="Restart headscale?"
        message="The control plane is briefly unavailable while it restarts. Established tunnels keep running; devices reconnect automatically."
        confirmText="Restart"
      />
    </Page>
  )
}
