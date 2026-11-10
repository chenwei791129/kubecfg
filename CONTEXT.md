# kubecfg

管理 kubeconfig 中 context 的分組與可見性，讓維運人員能在 agent 工作期間把 production context 從 kubectl 的視野中移除，防止誤操作。

## Language

**Context**:
kubectl 的 context 條目（名稱對應到一組 cluster、user、namespace）。kubecfg 以 context 為最小管理單位。
_Avoid_: cluster、環境、連線

**Group**:
使用者自訂名稱的 context 集合。一個 context 最多只屬於一個 Group；Group 是啟用／停用的唯一單位。
_Avoid_: tag、label、environment、profile

**Ungrouped**:
不屬於任何 Group 的 Context。永遠留在 Active kubeconfig 中，無法被停用。
_Avoid_: default group、unassigned

**Enabled**:
Group 的狀態之一：其所有 Context 都存在於 Active kubeconfig，kubectl 看得到。
_Avoid_: on、visible、unlocked

**Disabled**:
Group 的狀態之一：其所有 Context 都已從 Active kubeconfig 移除，kubectl 看不到。
_Avoid_: off、hidden、locked

**Active kubeconfig**:
kubectl 實際讀取的那個檔案（`$KUBECONFIG` 指定的單一路徑，否則為 `~/.kube/config`）。kubecfg 只修改它，不取代它。
_Avoid_: config、main config、kubeconfig（單獨使用時）

**Stash**:
存放 Disabled Group 的 Context 及其 cluster、user 條目的檔案，本身是合法的 kubeconfig，自包含、可獨立還原。明文存放，不提供保密。
_Avoid_: vault、backup、parked、hidden config

**Drift**:
Group 的狀態與 Active kubeconfig 的實際內容不一致，例如 Disabled Group 的 Context 出現在 Active kubeconfig 中。由 `status` 回報，由重跑 enable／disable 修正。
_Avoid_: out of sync、inconsistent、dirty
