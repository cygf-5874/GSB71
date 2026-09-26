# layercfg

分层配置合并的小库（Go 1.24，仅标准库，`go.mod` 无 `require`）。

配置通常是一叠层：默认值、环境文件、命令行覆盖、特性开关……`layercfg` 把这一叠层
按优先级合并成一棵配置树，并把它装进一个**快照**里交给调用方读取。

```go
low := layercfg.Layer{"app": layercfg.Layer{"port": 80, "debug": false}}
high := layercfg.Layer{"app": layercfg.Layer{"port": 9090}, "feature": layercfg.Layer{"on": true}}

s := layercfg.Merge(low, high)
port, _ := s.Lookup("app.port") // 9090
keys := s.Keys()                // ["app.debug", "app.port", "feature.on"]（升序）
```

也可以边攒边合并：

```go
st := layercfg.NewStore()
st.Add(low)
st.Add(high)
s := st.Snapshot()
```

## 怎么跑

```bash
go test ./...                          # 既有用例
go test -bench BenchmarkMerge -benchmem
go run ./check                         # 验收场景（固定件）
go run ./check -list                   # 列出全部场景
go run ./check --only cost
bash scripts/check.sh                  # 同上，薄封装
```

前提：Go 1.24。不引入任何第三方依赖。

## 对外契约

下面 10 条是 `layercfg` 的对外契约。**它们是契约，不是「当前行为」的转述**：
其中若干条现在的实现并没有满足。除特别说明外，对象一律是 `map[string]any`（`Layer` 是它的别名），
数组是 `[]any`。

1. **快照冻结**：`Merge(layers ...Layer) *Snapshot` 与 `Store.Snapshot()` 返回的快照，
   一旦返回即**冻结**；此后任何 `Merge` / `Store.Add` / `Store.Snapshot` 调用都不得改变
   它已经能观察到的内容 —— `Lookup` 的取值与 `Keys()` 的列表都不变。
2. **优先级**：层按**从低到高**合并，同一路径上高优先级的标量覆盖低优先级的标量。
   `Merge(layers...)` 里靠前的层优先级低；`Store` 里先 `Add` 的层优先级低。
3. **对象递归合并**：两层在同一路径上**都是对象**时逐键递归合并；一方是对象、另一方不是时，
   后者整个取代前者 —— 该路径下低层原有的子键全部消失。
4. **数组整体替换**：`[]any` 一律整体替换，**不逐元素合并**。
5. **`nil` 是删除标记**：高层某个键的值是 `nil` 时，删掉低层同名键**及其整棵子树**；
   最终 `Lookup` 报「不存在」，该键及其子路径都不出现在 `Keys()` 里。
6. **路径**：`Lookup("a.b.c")` 按点路径下钻。路径段本身含 `.` `[` `]` `"` `\` 时，
   用 `["…"]` 转义形式书写（段内的 `"` 与 `\` 前加一个 `\`）。`ParsePath` 按同一文法切段；
   语法是 `seg ('.' seg)*`，裸段不含 `.` 与 `[` 且非空。空路径、空段、格式非法的方括号段
   返回 `ErrBadPath`。**路径非法、指向对象、或已被删除时，`Lookup` 返回 `(nil, false)`**
   （不 panic、不返回错误）。
7. **`Keys()`**：返回**字典序升序**（按字节比较）的全部**叶子**路径；对象向下展开，
   标量与数组算叶子，空对象不产生任何路径。同一个快照多次调用 `Keys()` 结果逐项相同，
   且与 map 迭代顺序无关（跨进程一致）。
8. **只读共享**：`Lookup` / `Keys` / `ParsePath` 都不修改快照；同一个已冻结的快照
   可以被多个 goroutine 同时 `Lookup`，结果与单线程一致。
9. **`Merge` 不改动入参**：调用前后，传入的每个 `Layer`（含嵌套对象与数组）逐键、逐元素相同；
   `Store.Add` 同理。
10. **分配预算**：合并 N 层、每层 M 个键时，**分配次数不得随 N×M 线性增长**。固定件的判据是：
    合并 6 层 × 512 键时 `testing.AllocsPerRun` ≤ 12，并且「6 层」的分配次数不超过
    「1 层」的 3 倍。空层、空 `Store`、重复键都不报错；不访问网络与磁盘，不依赖时间与随机源。

> 已导出的类型、函数与方法签名是契约的一部分：`Layer`、`ErrBadPath`、`ParsePath`、`Merge`、
> `NewStore`、`Store.Add`、`Store.Snapshot`、`Snapshot.Lookup`、`Snapshot.Keys` 都不得改变；
> 可以新增其它类型、方法与字段。

## 验收

`check/` 是固定验收程序，**不要修改**。场景分四组，共 10 个：

| 组 | 场景 | 覆盖 |
| --- | --- | --- |
| `semantics` | `SEM1_scalar_override` | 契约 2：多层标量按优先级覆盖 |
| `semantics` | `SEM2_object_recursive_merge` | 契约 3：对象递归合并 / 标量与对象互替 |
| `semantics` | `SEM3_array_replaced_wholesale` | 契约 4：数组整体替换 |
| `semantics` | `SEM4_nil_delete_and_paths` | 契约 5、6：删除标记、点路径与转义路径、`ParsePath` 报错 |
| `frozen` | `FRZ1_old_snapshot_stable` | 契约 1：已返回的快照不受后续合并影响 |
| `frozen` | `FRZ2_keys_sorted_and_stable` | 契约 7：`Keys()` 升序且多次调用稳定 |
| `cost` | `COST1_allocs_under_budget` | 契约 10：`AllocsPerRun` ≤ 12 |
| `cost` | `COST2_alloc_growth_bounded` | 契约 10：6 层分配次数 ≤ 1 层的 3 倍 |
| `cost` | `COST3_semantic_parity_with_deepcopy` | 契约 3、4、5、7、10：与逐层深拷贝参照物逐键对拍 |
| `api` | `API1_merge_does_not_mutate_layers` | 契约 9：`Merge` / `Store` 不改动入参 |

`--only semantics|frozen|cost|api` 可以只跑一组。每个场景在**独立子进程**里执行，
挂死不会遮蔽其余场景；子进程带 25 秒看门狗与 goroutine 栈转储（超时以退出码 3 结束）。
判据全部是确定性的：逐键比对 + `testing.AllocsPerRun` 的**分配次数**，不依赖墙钟或机器速度。

## 目录

```
.
├── go.mod               module layercfg
├── errors.go            ErrBadPath
├── tree.go              Layer、深拷贝、路径文法
├── merge.go             逐层合并规则
├── store.go             Store 与 Snapshot
├── layercfg_test.go     既有用例
├── bench_test.go        BenchmarkMerge
├── check/main.go        固定验收程序（勿改）
├── scripts/check.sh     薄封装：go run ./check "$@"
└── README.md
```

## User Prompt

```
每次合并都要深拷贝一整棵树 —— layercfg 目前就是这么干的，合并 6 层、每层 512 个键时，
`BenchmarkMerge` 报出来的分配次数是层数乘以键数的量级。它是个 Go 1.24 的分层配置合并库，
只用标准库，跑 `go test` 与 `go run ./check`（`check` 是固定验收程序，别改）。

`layercfg_test.go` 里 14 个用例当前全绿，但它们只检查合并后的值 —— 既没检查过分配的代价，
也没检查过「已经拿到的旧快照会不会被后来的合并改掉」。

任务：把 README「对外契约」的 10 条补齐，让既有用例与固定件全过。

验收：
- go test ./... 退出码 0；
- go run ./check 退出码 0，10 个场景全过（semantics 4 + frozen 2 + cost 3 + api 1）。

约束：
1. 不改 `check/`、不改既有用例的断言；已公开的函数签名不变（可新增）。
2. 不引入第三方依赖，`go.mod` 里不许出现 require。
3. 不许靠「少做点事」达标：合并结果必须与逐层深拷贝的参照实现逐键相同，`cost` 组里带语义对拍。
4. 分配判据卡的是次数不是时间（`testing.AllocsPerRun`），跑题机快慢不影响结果。
```
