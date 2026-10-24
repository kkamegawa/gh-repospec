# トラブルシューティングとFAQ

[English](troubleshooting.md)

よく発生するエラーとplanのエントリーを、表示内容、原因、対処の順にまとめています。特に、
`import`で作成した設定を別のリポジトリへapplyする場合に注意が必要です。各ケースの確定済み
の動作は[design_ja.md](design_ja.md)に記録しています。

失敗した`apply`を再実行する前に、必ず`plan`をもう一度実行してください。`apply`は
トランザクションではありません。エラーより前に書き込まれた変更は反映されたままで、後続の
ステージ(最後に実行されるrulesetsなど)には到達していません。新しいplanで残りの変更を
確認できます。

## 別のリポジトリへ設定を適用する場合

`import`は、読み取り元のリポジトリの状態を表します。可視性、料金プラン、作成日が異なる
リポジトリへ同じファイルを適用すると、読み取り元にはなかったGitHubの制限に該当する場合が
あります。import結果を再利用する前、特にpublicからprivateのリポジトリへ移す場合は、以下を
確認してください。

### HTTP 422「Please ensure the billing plan supports the required reviewers protection rule」

```text
error: ensure Actions environment "copilot": PUT /repos/OWNER/REPO/environments/copilot
returned HTTP 422: Failed to create the environment protection rule. Please ensure the
billing plan supports the required reviewers protection rule.
```

**原因** GitHub Free、Pro、Teamプランでは、environmentのwait timerとrequired reviewersを
publicリポジトリでしか使えません。GitHubはリクエスト内の`wait_timer`、
`prevent_self_review`、`reviewers`を保護ルールの書き込みとして扱うため、保護を無効にする
値であってもprivateリポジトリでは拒否されます。`import`は保護のないenvironmentに対して
`waitTimer: 0`、`preventSelfReview: false`、`reviewers: []`を出力します。このため、public
リポジトリからimportしたファイルをprivateリポジトリに適用すると、このエラーになります。

**対処** [#51](https://github.com/kkamegawa/gh-repospec/pull/51)を含むバージョンでは、
適用前に現在のenvironmentを読み取り、解除すべき既存ルールがある場合だけ無効化の値を送信します。
`gh extension upgrade repospec`で拡張機能を更新してください。

古いバージョンでは、該当するenvironmentから上記3項目と空の`customProtectionRules: []`を
削除します。

```yaml
environments:
  - name: copilot
    deploymentBranchPolicy:
      allowAllBranches: true
      protectedBranches: false
      customBranchPolicies: false
```

省略したフィールドは「無効」ではなく「管理対象外」です。このため、既に保護されている
environmentの保護は解除されません。

privateリポジトリで実際にreviewersやwait timerを使いたい場合、このエラーは本当のプラン制限
です。GitHub Enterpriseを使うか、リポジトリをpublicにしてください。このツールは`apply`を
成功させるために、要求された保護ルールを勝手に外すことはしません。

### privateリポジトリで`actions.forkPullRequestApproval`がblockedになる

**原因** GitHubはprivateリポジトリに対するfork pull request approvalのエンドポイントを
HTTP 422で拒否します。このため、`repository.visibility`が`private`の場合、`plan`はこの
フィールドをblockedにします。

**対処** privateリポジトリ向けの設定から`actions.forkPullRequestApproval`を削除します。

### publicリポジトリで`actions.workflowAccessLevel`がblockedになる

**原因** 前の項目の逆のケースです。workflow accessはprivateとinternalのリポジトリにだけ
適用され、publicリポジトリにはGitHubがHTTP 422を返します。`import`もprivateまたはinternal
のリポジトリからだけ読み取ります。

**対処** 適用先がpublicの場合は`actions.workflowAccessLevel`を削除します。

### `repository.visibility`が「allowVisibilityChange is required」でblockedになる

**原因** 可視性の変更は意図的に保護されています。リポジトリの状態からは変更の意図を
判断できないため、`import`は`allowVisibilityChange`を出力しません。

**対処** 変更が意図したものであることを確認してから、
`repository.allowVisibilityChange: true`を追加します。

### CodeQL default setupやAI Scanがblocked、manual、または利用不可になる

**原因** GitHub Advanced Securityのないprivateリポジトリは、CodeQL default setupとAI Scanの
読み取りに403を返します。AI Scanは404を返す場合もあります。AI Scanには、CodeQL default
setup、Copilotライセンス、enterpriseとorganizationでの有効化も必要です。

**対処** 適用先にAdvanced Securityがない可能性がある場合は、これらの機能に
`optional: true`を指定します。有効化を指定していてもmanualとして報告され、実行は止まりません。
利用できない機能を無効化する指定は既に満たされているため、noopになります。

### `actions.oidcSubjectCustomization.useImmutableSubject`がMANUALになる

**原因** GitHubは2026-07-15以降に作成または名前変更されたすべてのリポジトリで、immutable
なOIDC subject形式を有効にします。これを無効にする書き込みは、エラーにならずに無視されます。
古いリポジトリからimportしたファイルは`false`を指定しているため、新しいリポジトリでは収束
しません。

**対処** 対応は不要です。OIDCカスタマイズのそれ以外の設定は適用され、manualのエントリーは
`apply`や`plan --ci`を失敗させません。エントリーを消したい場合は、このフィールドを`true`に
するか削除します。

### `repository.hasDownloads`がblockedになる

**原因** GitHubのリポジトリ更新APIでは、既存リポジトリの`has_downloads`を変更できません。
この値はリポジトリの作成時だけ反映されます。

**対処** フィールドを削除するか、現在の値に合わせます。

### 適用先environmentでcustom deployment protectionのアプリを利用できない

**原因** custom deployment protection ruleが参照するGitHub Appが、適用先リポジトリに
インストールされていないか、利用できません。また、Free、Pro、Teamプランではcustom
protection ruleもpublicリポジトリに限られます。

**対処** 適用先にアプリをインストールするか、`customProtectionRules`からそのslugを削除します。

### rulesetのbypass actorを解決できない

**原因** bypass actorのslugや名前は、書き込み前に数値IDへ解決されます。適用先のownerに
存在しないapp、team、custom role、または認証ユーザーから見えないprivate appは、bypassを
黙って削除せずにplanをblockedにします。

**対処** 適用先に存在するactorを使うか、private appには解決済みの`id:<number>`形式を
使います。

## 入力とローカルの意図

### secretが「source environment variable is not set」でblockedになる

**原因** secretの値は設定ファイルに保存されず、GitHubからも返されません。各secretの値は、
`sourceEnv`で指定した環境変数から読み取ります。`import`はsecret名だけを、コメントまたは
diagnosticとして報告します。

**対処** `plan`や`apply`の実行前にその環境変数を設定するか、ファイルからsecretを削除します。

### secretの更新が収束済みにならない

**原因** GitHubはsecretの値を返さないため、source環境変数が設定されたsecretは常に値を
確認できない更新(opaque)として扱われます。`plan --ci`のドリフト判定からは除外されます。

**対処** これは想定どおりの動作です。一度だけ書き込むには`updatePolicy: createOnly`を、
設定済みのすべてのsecretを1回の実行でローテーションするには`apply --force-secrets`を使います。

### 「seed conflicts with existing file」になる

**原因** seedのファイルが、リポジトリにある内容の異なる既存ファイルを上書きしようとして
います。`apply`は暗黙に上書きしません。

**対処** 置き換えが意図どおりであればパスを`seed.overwrite`に追加するか、seedからその
ファイルを削除します。

### 「cannot set default branch on an empty repository without seed files or commands」になる

**原因** GitHubでは、コミットでブランチが作成されるまでdefault branchを選択できません。

**対処** `seed`または`commands`を追加するか、リポジトリに内容ができるまで
`repository.defaultBranch`を削除します。

### commandが「requires --approve-command」と表示される

**原因** 宣言したすべてのcommandは、`--yes`を指定していても個別の承認が必要です。

**対処** 内容を確認したcommandごとに`--approve-command <id>`を指定します。

## 検証と依存関係

### `allowedActions: selected`が拒否される

**原因** `actions.selectedActions`がないとGitHubのallowlistは空になり、Actionを使う
すべてのworkflowがブロックされます。

**対処** `githubOwnedAllowed`、`verifiedAllowed`、必要に応じて`patternsAllowed`を含む
`selectedActions`を追加します。

### Dependabot security updatesやAI Scanが依存関係でblockedになる

**原因** Dependabot security updatesにはDependabot alertsが、AI ScanにはCodeQL default
setupが必要です。依存する機能を有効にしながら、依存先に`enabled: false`を明示すると矛盾します。

**対処** 依存先を有効にするか、現在の状態を使うために依存先の指定を省略します。

### deployment branch patternに`customBranchPolicies`が必要になる

**原因** GitHubは、custom branch policiesが有効な場合だけ、environmentのbranchとtagの
パターンを扱います。

**対処** `branchPolicies`を宣言するenvironmentに
`deploymentBranchPolicy.customBranchPolicies: true`を指定します。

## GitHub APIの応答

### 403がレート制限として報告される

**原因** GitHubは一部の403と429の応答を、プライマリまたはセカンダリのレート制限として
示します。これは機能の利用可否とは関係がないため、`plan`は状態を不明として報告し、`import`は
失敗します。

**対処** レート制限が解除されるまで待ってから、コマンドを再実行します。

### `plan --ci`が1または2で終了する

**原因** `1`は、blockedのplanを含むコマンドの失敗を表します。`2`は、`apply`で収束できる
差分が1つ以上見つかったことを表します。manualのエントリーとopaqueなsecret更新では`2`に
なりません。

**対処** planの出力を確認し、先にblockedのエントリーを解消してから、残りの変更をapplyします。
