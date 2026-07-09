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
  Button,
  ButtonText,
  HStack,
  VStack,
  Text
} from '@spr-networks/plugin-ui'

const BASE = `/plugins/${api.pluginURI() || 'spr-headscale'}`

const dash = (v) => v || '—'

export default function Plugin() {
  const alert = useAlert()
  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState(null)
  const [config, setConfig] = useState(null)
  const [users, setUsers] = useState([])
  const [nodes, setNodes] = useState([])
  const [keys, setKeys] = useState([])

  // settings form
  const [serverURL, setServerURL] = useState('')
  const [baseDomain, setBaseDomain] = useState('')
  const [magicDNS, setMagicDNS] = useState(true)
  const [derpEnabled, setDerpEnabled] = useState(true)

  // users
  const [newUser, setNewUser] = useState('')
  const [deleteUser, setDeleteUser] = useState(null)

  // preauth keys
  const [keyUser, setKeyUser] = useState(null) // user object for the generator modal
  const [keyReusable, setKeyReusable] = useState(false)
  const [keyEphemeral, setKeyEphemeral] = useState(false)
  const [keyExpiration, setKeyExpiration] = useState('1h')
  const [createdKey, setCreatedKey] = useState(null) // copy-once modal

  // nodes
  const [deleteNode, setDeleteNode] = useState(null)
  const [expireNode, setExpireNode] = useState(null)

  const refresh = () => {
    Promise.allSettled([
      api.get(`${BASE}/status`),
      api.get(`${BASE}/config`),
      api.get(`${BASE}/users`),
      api.get(`${BASE}/nodes`),
      api.get(`${BASE}/preauthkeys`)
    ]).then(([s, c, u, n, k]) => {
      if (s.status === 'fulfilled') setStatus(s.value)
      if (c.status === 'fulfilled' && c.value) {
        setConfig(c.value)
        setServerURL(c.value.ServerURL || '')
        setBaseDomain(c.value.BaseDomain || '')
        setMagicDNS(!!c.value.MagicDNS)
        setDerpEnabled(!!c.value.DERPEnabled)
      }
      if (u.status === 'fulfilled') setUsers(u.value || [])
      if (n.status === 'fulfilled') setNodes(n.value || [])
      if (k.status === 'fulfilled') setKeys(k.value || [])
      if (s.status === 'rejected') {
        alert.error('Failed to reach spr-headscale backend', s.reason)
      }
      setLoading(false)
    })
  }

  useEffect(() => {
    refresh()
  }, [])

  const saveConfig = () => {
    api
      .put(`${BASE}/config`, {
        ServerURL: serverURL.trim(),
        BaseDomain: baseDomain.trim(),
        MagicDNS: magicDNS,
        DERPEnabled: derpEnabled
      })
      .then(() => {
        alert.success('Saved — headscale restarted')
        refresh()
      })
      .catch((err) => alert.error('Failed to save config', err))
  }

  const restart = () => {
    api
      .post(`${BASE}/restart`)
      .then(() => {
        alert.success('headscale restarted')
        refresh()
      })
      .catch((err) => alert.error('Restart failed', err))
  }

  const addUser = () => {
    let name = newUser.trim()
    if (!name) {
      return alert.warning('Enter a user name')
    }
    api
      .post(`${BASE}/users`, { Name: name })
      .then(() => {
        alert.success(`User ${name} created`)
        setNewUser('')
        refresh()
      })
      .catch((err) => alert.error('Failed to create user', err))
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

  const createKey = () => {
    let u = keyUser
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
      .catch((err) => alert.error('Failed to create preauth key', err))
  }

  const copyKey = () => {
    if (createdKey?.Key && navigator?.clipboard?.writeText) {
      navigator.clipboard
        .writeText(createdKey.Key)
        .then(() => alert.success('Key copied to clipboard'))
        .catch(() => alert.warning('Copy failed — select and copy manually'))
    } else {
      alert.warning('Clipboard unavailable — select and copy manually')
    }
  }

  const doExpireNode = () => {
    let n = expireNode
    setExpireNode(null)
    api
      .post(`${BASE}/nodes/${n.ID}/expire`)
      .then(() => {
        alert.success(`Node ${n.GivenName || n.Name} expired`)
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

  if (loading) {
    return (
      <Page>
        <Loading />
      </Page>
    )
  }

  return (
    <Page>
      <ListHeader
        title="Headscale"
        description="Self-hosted Tailscale control server"
      >
        <Button size="sm" variant="outline" onPress={restart}>
          <ButtonText>Restart</ButtonText>
        </Button>
      </ListHeader>

      <Card>
        <SectionHeader
          title="Status"
          right={<StatusDot online={!!status?.Running} />}
        />
        <HStack flexWrap="wrap" gap="$2">
          <StatTile label="State" value={status?.Running ? 'Running' : 'Stopped'} />
          <StatTile
            label="Version"
            value={status?.Version || status?.PinnedVersion || '—'}
            mono
          />
          <StatTile label="Users" value={String(users.length)} />
          <StatTile label="Nodes" value={String(nodes.length)} />
          <StatTile
            label="Online"
            value={String(nodes.filter((n) => n.Online).length)}
          />
        </HStack>
        <VStack space="xs" mt="$3">
          <KeyVal label="Server URL" value={dash(status?.ServerURL)} mono />
          <KeyVal label="Listening on" value={dash(status?.ListenAddr)} mono />
          <KeyVal
            label="MagicDNS"
            value={
              status?.MagicDNS ? `on (${status?.BaseDomain})` : 'off'
            }
          />
          <KeyVal
            label="DERP relays"
            value={status?.DERPEnabled ? 'tailscale default map' : 'disabled'}
          />
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="Users" count={users.length} />
        <VStack space="sm">
          {users.length === 0 ? (
            <EmptyState
              title="No users yet"
              description="Create a user, then generate a preauth key to join devices."
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
                      id {u.ID}
                      {' · '}
                      {nodes.filter((n) => n.UserID === u.ID).length} node(s)
                    </Text>
                  </VStack>
                  <HStack space="sm">
                    <Button
                      size="xs"
                      variant="outline"
                      onPress={() => {
                        setKeyReusable(false)
                        setKeyEphemeral(false)
                        setKeyExpiration('1h')
                        setKeyUser(u)
                      }}
                    >
                      <ButtonText>New key</ButtonText>
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
            <Button size="sm" onPress={addUser}>
              <ButtonText>Add</ButtonText>
            </Button>
          </HStack>
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="Preauth keys" count={keys.length} />
        {keys.length === 0 ? (
          <EmptyState
            title="No preauth keys"
            description="Use 'New key' on a user to generate one. Keys are shown in full only once, at creation."
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
                    </Text>
                  </VStack>
                  <Text size="xs" color={k.Expired ? '$red500' : '$muted500'}>
                    {k.Expired
                      ? 'expired'
                      : k.Expires
                        ? `expires ${new Date(k.Expires).toLocaleString()}`
                        : ''}
                  </Text>
                </HStack>
              </ListItem>
            ))}
          </VStack>
        )}
      </Card>

      <Card>
        <SectionHeader title="Nodes" count={nodes.length} />
        {nodes.length === 0 ? (
          <EmptyState
            title="No nodes"
            description="Join a device with: tailscale up --login-server <Server URL> --authkey <preauth key>"
          />
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
                      <ButtonText>Expire</ButtonText>
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

      <Card>
        <SectionHeader title="Settings" />
        <VStack space="md">
          <TextField
            label="Server URL"
            value={serverURL}
            onChangeText={setServerURL}
            placeholder={`http://${status?.ContainerIP || 'container-ip'}:8080`}
            helper="URL tailscale clients use to reach headscale. Leave empty to use the container IP (LAN-only). For public access set your reverse-proxied https:// URL."
          />
          <TextField
            label="MagicDNS base domain"
            value={baseDomain}
            onChangeText={setBaseDomain}
            placeholder="headscale.internal"
            helper="Node hostnames become <name>.<base domain>. Must differ from the Server URL host."
          />
          <HStack justifyContent="space-between" alignItems="center">
            <Text>MagicDNS</Text>
            <Toggle value={magicDNS} onPress={() => setMagicDNS(!magicDNS)} />
          </HStack>
          <HStack justifyContent="space-between" alignItems="center">
            <VStack flex={1} pr="$4">
              <Text>DERP relays</Text>
              <Text size="xs" color="$muted500">
                Use Tailscale's public relay map for NAT traversal help.
                Disable for direct connections only.
              </Text>
            </VStack>
            <Toggle
              value={derpEnabled}
              onPress={() => setDerpEnabled(!derpEnabled)}
            />
          </HStack>
          <HStack justifyContent="flex-end">
            <Button size="sm" onPress={saveConfig}>
              <ButtonText>Save & apply</ButtonText>
            </Button>
          </HStack>
        </VStack>
      </Card>

      {/* per-user preauth key generator */}
      <ModalForm
        isOpen={!!keyUser}
        onClose={() => setKeyUser(null)}
        title={`New preauth key for ${keyUser?.Name || ''}`}
      >
        <VStack space="md">
          <TextField
            label="Expiration"
            value={keyExpiration}
            onChangeText={setKeyExpiration}
            placeholder="1h"
            helper="Duration like 30m, 24h, 90d"
          />
          <HStack justifyContent="space-between" alignItems="center">
            <Text>Reusable</Text>
            <Toggle
              value={keyReusable}
              onPress={() => setKeyReusable(!keyReusable)}
            />
          </HStack>
          <HStack justifyContent="space-between" alignItems="center">
            <Text>Ephemeral</Text>
            <Toggle
              value={keyEphemeral}
              onPress={() => setKeyEphemeral(!keyEphemeral)}
            />
          </HStack>
          <Button size="sm" onPress={createKey}>
            <ButtonText>Generate key</ButtonText>
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
            Copy this key now — it will not be shown again.
          </Text>
          <Text
            fontFamily="$mono"
            size="sm"
            selectable
            p="$2"
            bg="$backgroundContentLight"
            sx={{ _dark: { bg: '$backgroundContentDark' } }}
          >
            {createdKey?.Key}
          </Text>
          <Text size="xs" color="$muted500">
            {createdKey?.UserName}
            {createdKey?.Reusable ? ' · reusable' : ' · single-use'}
            {createdKey?.Ephemeral ? ' · ephemeral' : ''}
            {createdKey?.Expires
              ? ` · expires ${new Date(createdKey.Expires).toLocaleString()}`
              : ''}
          </Text>
          <HStack space="sm" justifyContent="flex-end">
            <Button size="sm" variant="outline" onPress={copyKey}>
              <ButtonText>Copy</ButtonText>
            </Button>
            <Button size="sm" onPress={() => setCreatedKey(null)}>
              <ButtonText>Done</ButtonText>
            </Button>
          </HStack>
        </VStack>
      </ModalForm>

      <ModalConfirm
        isOpen={!!deleteUser}
        onClose={() => setDeleteUser(null)}
        onConfirm={doDeleteUser}
        title={`Delete user ${deleteUser?.Name}?`}
        message="This destroys the user in headscale. Their nodes must be removed first."
        confirmText="Delete"
        destructive
      />

      <ModalConfirm
        isOpen={!!expireNode}
        onClose={() => setExpireNode(null)}
        onConfirm={doExpireNode}
        title={`Expire node ${expireNode?.GivenName || expireNode?.Name}?`}
        message="The node keeps its registration but must re-authenticate before reconnecting."
        confirmText="Expire"
      />

      <ModalConfirm
        isOpen={!!deleteNode}
        onClose={() => setDeleteNode(null)}
        onConfirm={doDeleteNode}
        title={`Remove node ${deleteNode?.GivenName || deleteNode?.Name}?`}
        message="This deletes the node from headscale. It will need a new preauth key to rejoin."
        confirmText="Remove"
        destructive
      />
    </Page>
  )
}
