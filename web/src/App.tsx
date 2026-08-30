import { Navigate, Route, Routes } from 'react-router-dom'
import { ChannelView, DmView, DmByIdView, NoChannelSelected } from './routes/ChannelView'
import { AdminSessions } from './routes/AdminSessions'
import { ApiKeys } from './routes/ApiKeys'
import { AppShell } from './routes/AppShell'
import { Login } from './routes/Login'
import { Setup } from './routes/Setup'

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/setup" element={<Setup />} />
      <Route path="/" element={<AppShell />}>
        <Route index element={<NoChannelSelected />} />
        <Route path="c/:slug" element={<ChannelView />} />
        <Route path="dm/:username" element={<DmView />} />
        <Route path="dm/id/:id" element={<DmByIdView />} />
        <Route path="api-keys" element={<ApiKeys />} />
        <Route path="admin/sessions" element={<AdminSessions />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
