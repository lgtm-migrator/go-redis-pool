package pool

import (
	"time"

	redis "github.com/go-redis/redis/v7"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pool", func() {
	var haPool *Pool
	var shardPool *Pool
	var err error
	var pools []*Pool

	BeforeEach(func() {
		haConfig := &HAPoolConfig{
			Master: &NodeConfig{
				Addr: "127.0.0.1:8379",
			},
			Slaves: []*NodeConfig{
				&NodeConfig{
					Addr: "127.0.0.1:8380",
				},
				&NodeConfig{
					Addr: "127.0.0.1:8381",
				},
			},
		}
		haConfig1 := &HAPoolConfig{
			Master: &NodeConfig{
				Addr: "127.0.0.1:8382",
			},
			Slaves: []*NodeConfig{
				&NodeConfig{
					Addr: "127.0.0.1:8383",
				},
			},
		}

		haPool, err = NewHAPool(haConfig)
		Expect(err).NotTo(HaveOccurred())
		master, _ := haPool.WithMaster()
		Expect(master.FlushDB().Err()).NotTo(HaveOccurred())

		shardPool, err = NewShardPool(&ShardPoolConfig{
			Shards: []*HAPoolConfig{
				haConfig,
				haConfig1,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		shards := shardPool.connFactory.(*ShardConnFactory).shards
		for _, shard := range shards {
			master, _ = shard.getMasterConn()
			Expect(master.FlushDB().Err()).NotTo(HaveOccurred())
		}
		pools = []*Pool{haPool, shardPool}
	})

	AfterEach(func() {
		haPool.Close()
		shardPool.Close()
	})

	Describe("Commands", func() {

		It("ping", func() {
			for _, pool := range pools {
				_, err := pool.Ping().Result()
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("get/set", func() {
			for _, pool := range pools {
				result := pool.Set("foo", "bar", 0)
				Expect(result.Val()).To(Equal("OK"))
				// wait for master progressing the set result
				time.Sleep(10 * time.Millisecond)
				Expect(pool.Get("foo").Val()).To(Equal("bar"))
			}
		})

		It("echo", func() {
			Expect(haPool.Echo("hello").Err()).NotTo(HaveOccurred())
			Expect(shardPool.Echo("hello").Err()).To(Equal(errShardPoolUnSupported))
		})

		It("delete", func() {
			keys := []string{"a0", "b0", "c0", "d0"}
			for _, pool := range pools {
				for _, key := range keys {
					Expect(pool.Set(key, "value", 0).Err()).NotTo(HaveOccurred())
				}
				deleteKeys := append(keys, "e")
				n, err := pool.Del(deleteKeys...)
				Expect(err).NotTo(HaveOccurred())
				Expect(int(n)).To(Equal(len(keys)))
			}
		})

		It("unlink", func() {
			keys := []string{"a1", "b1", "c1", "d1"}
			for _, pool := range pools {
				for _, key := range keys {
					Expect(pool.Set(key, "value", 0).Err()).NotTo(HaveOccurred())
				}
				unlinkKeys := append(keys, "e1")
				n, err := pool.Unlink(unlinkKeys...)
				Expect(err).NotTo(HaveOccurred())
				Expect(int(n)).To(Equal(len(keys)))
			}
		})

		It("touch", func() {
			keys := []string{"a2", "b2", "c2", "d2"}
			for _, pool := range pools {
				for _, key := range keys {
					Expect(pool.Set(key, "value", 0).Err()).NotTo(HaveOccurred())
				}
				touchKeys := append(keys, "e2")
				n, err := pool.Touch(touchKeys...)
				Expect(err).NotTo(HaveOccurred())
				Expect(int(n)).To(Equal(len(keys)))
				pool.Del(keys...)
			}
		})

		It("mget", func() {
			keys := []string{"a3", "b3", "c3", "d3"}
			for _, pool := range pools {
				for _, key := range keys {
					Expect(pool.Set(key, key, 0).Err()).NotTo(HaveOccurred())
				}
				time.Sleep(10 * time.Millisecond)
				mgetKeys := append(keys, "e3")
				vals, err := pool.MGet(mgetKeys...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(vals)).To(Equal(len(keys) + 1))
				for i := 0; i < len(keys); i++ {
					Expect(vals[i].(string)).To(Equal(keys[i]))
				}
				Expect(vals[len(keys)]).To(BeNil())
				pool.Del(keys...)
			}
		})

		It("exists", func() {
			keys := []string{"a4", "b4", "c4", "d4"}
			for _, pool := range pools {
				for _, key := range keys {
					Expect(pool.Set(key, "value", 0).Err()).NotTo(HaveOccurred())
				}
				existsKeys := append(keys, "e4")
				n, err := pool.Exists(existsKeys...)
				Expect(err).NotTo(HaveOccurred())
				Expect(int(n)).To(Equal(len(keys)))
				pool.Del(keys...)
			}
		})

		It("mset", func() {
			kvs := []string{"key1", "value1", "key2", "value2", "key3", "value3"}
			keys := make([]string, 0)
			for i := 0; i < len(kvs); i += 2 {
				keys = append(keys, kvs[i])
			}
			for _, pool := range pools {
				Expect(pool.MSet(kvs).Err()).NotTo(HaveOccurred())
				time.Sleep(10 * time.Millisecond)
				vals, err := pool.MGet(keys...)
				Expect(err).NotTo(HaveOccurred())
				for i := 0; i < len(vals); i += 1 {
					Expect(vals[i].(string)).To(Equal(kvs[2*i+1]))
				}
				pool.Del(keys...)
			}
		})

		It("msetnx", func() {
			kvs := []string{"key1_nx", "value1", "key2_nx", "value2"}
			keys := make([]string, 0)
			for i := 0; i < len(kvs); i += 2 {
				keys = append(keys, kvs[i])
			}
			for _, pool := range pools {
				Expect(pool.MSetNX(kvs).Val()).To(Equal(true))
				Expect(pool.MSetNX(kvs).Val()).To(Equal(false))
				if pool == shardPool {
					Expect(pool.MSetNX(append(kvs, "key3_nx", "value3")).Err()).To(HaveOccurred())
				}
				time.Sleep(10 * time.Millisecond)
				vals, err := pool.MGet(keys...)
				Expect(err).NotTo(HaveOccurred())
				for i := 0; i < len(vals); i += 1 {
					Expect(vals[i].(string)).To(Equal(kvs[2*i+1]))
				}
				pool.Del(keys...)
			}
		})

		It("expire", func() {
			key := "expire_foo"
			for _, pool := range pools {
				result := pool.Set(key, "bar", 0)
				Expect(result.Val()).To(Equal("OK"))
				Expect(pool.Expire(key, 10*time.Second).Val()).To(Equal(true))
				time.Sleep(10 * time.Millisecond)
				Expect(pool.TTL(key).Val()).NotTo(Equal(-1))
				pool.Del(key)
			}
		})

		It("expire_at", func() {
			key := "expireat_foo"
			for _, pool := range pools {
				result := pool.Set(key, "bar", 0)
				Expect(result.Val()).To(Equal("OK"))
				Expect(pool.ExpireAt(key, time.Now().Add(10*time.Second)).Val()).To(Equal(true))
				time.Sleep(10 * time.Millisecond)
				Expect(pool.TTL(key).Val()).NotTo(Equal(-1))
				pool.Del(key)
			}
		})

		It("rename", func() {
			key := "rename_key"
			newKey := "rename_key_new"
			for _, pool := range pools {
				result := pool.Set(key, "bar", 0)
				Expect(result.Val()).To(Equal("OK"))
				result = pool.Rename(key, newKey)
				Expect(result.Val()).To(Equal("OK"))
				time.Sleep(10 * time.Millisecond)
				Expect(pool.Get(newKey).Val()).To(Equal("bar"))
				Expect(pool.Get(key).Val()).To(Equal(""))
				pool.Del(newKey)
			}
		})

		It("renamenx", func() {
			key := "renamenx_key"
			newKey := "renamenx_key_new4"
			for _, pool := range pools {
				if pool == shardPool {
					Expect(pool.Set(key, "bar", 0).Val()).To(Equal("OK"))
					Expect(pool.RenameNX(key, newKey).Val()).To(Equal(true))
					time.Sleep(10 * time.Millisecond)
					Expect(pool.Get(newKey).Val()).To(Equal("bar"))
					Expect(pool.Get(key).Val()).To(Equal(""))
					pool.Del(newKey)
				}
			}
		})

		It("type", func() {
			key := "type_key"
			for _, pool := range pools {
				Expect(pool.Set(key, "bar", 0).Val()).To(Equal("OK"))
				time.Sleep(10 * time.Millisecond)
				Expect(pool.Type(key).Val()).To(Equal("string"))
				pool.Del(key)
			}
		})

		It("append", func() {
			key := "append_key"
			for _, pool := range pools {
				Expect(pool.Append(key, "hello").Val()).To(Equal(int64(5)))
				Expect(pool.Append(key, "world").Val()).To(Equal(int64(10)))
				pool.Del(key)
			}
		})

		It("get range", func() {
			key := "getrange_key"
			for _, pool := range pools {
				Expect(pool.Set(key, "hello,world", 0).Val()).To(Equal("OK"))
				time.Sleep(10 * time.Millisecond)
				Expect(pool.GetRange(key, 2, 5).Val()).To(Equal("llo,"))
				pool.Del(key)
			}
		})

		It("getset", func() {
			key := "getset_key"
			for _, pool := range pools {
				Expect(pool.Set(key, "hello", 0).Val()).To(Equal("OK"))
				Expect(pool.GetSet(key, "world").Val()).To(Equal("hello"))
				pool.Del(key)
			}
		})

		It("get/set bit", func() {
			key := "setbit_key"
			offsets := []int64{1, 3, 5, 7, 15, 31, 63}
			for _, pool := range pools {
				for _, offset := range offsets {
					Expect(pool.SetBit(key, offset, 1).Val()).To(Equal(int64(0)))
				}
				time.Sleep(10 * time.Millisecond)
				for _, offset := range offsets {
					Expect(pool.GetBit(key, offset).Val()).To(Equal(int64(1)))
				}
				Expect(pool.BitPos(key, 1, 0, 64).Val()).To(Equal(int64(1)))
				Expect(pool.BitPos(key, 0, 0, 64).Val()).To(Equal(int64(0)))
				Expect(pool.BitCount(key, &redis.BitCount{
					Start: 0,
					End:   64,
				}).Val()).To(Equal(int64(len(offsets))))
				pool.Del(key)
			}
		})

		It("bit op", func() {
			key0 := "op_key0"
			key1 := "op_key1"
			key2 := "op_key_cross"
			destKey := "opDestKey"
			for _, pool := range pools {
				Expect(pool.SetBit(key0, 0, 1).Err()).NotTo(HaveOccurred())
				Expect(pool.SetBit(key1, 0, 1).Err()).NotTo(HaveOccurred())
				if pool == shardPool {
					Expect(pool.BitOpAnd(destKey, key0, key2).Err()).To(HaveOccurred())
				}
				Expect(pool.BitOpAnd(destKey, key0, key1).Err()).NotTo(HaveOccurred())
				Expect(pool.GetBit(destKey, 0).Val()).To(Equal(int64(1)))
				Expect(pool.BitOpOr(destKey, key0, key1).Err()).NotTo(HaveOccurred())
				Expect(pool.GetBit(destKey, 0).Val()).To(Equal(int64(1)))
				Expect(pool.BitOpXor(destKey, key0, key1).Err()).NotTo(HaveOccurred())
				Expect(pool.GetBit(destKey, 0).Val()).To(Equal(int64(0)))
				Expect(pool.BitOpNot(destKey, key0).Err()).NotTo(HaveOccurred())
				Expect(pool.GetBit(destKey, 0).Val()).To(Equal(int64(0)))
				pool.Del(key0, key1, destKey)
			}
		})

		It("incr/decr", func() {
			key := "incr_key"
			for _, pool := range pools {
				Expect(pool.Set(key, 100, 0).Err()).NotTo(HaveOccurred())
				Expect(pool.Incr(key).Val()).To(Equal(int64(101)))
				Expect(pool.Decr(key).Val()).To(Equal(int64(100)))
				Expect(pool.IncrBy(key, 100).Val()).To(Equal(int64(200)))
				Expect(pool.DecrBy(key, 100).Val()).To(Equal(int64(100)))
				pool.Del(key)
			}
		})

		It("incrbyfloat", func() {
			key := "incrbyfloat_key"
			for _, pool := range pools {
				Expect(pool.Set(key, 100, 0).Err()).NotTo(HaveOccurred())
				Expect(pool.IncrByFloat(key, 1.5).Val()).To(Equal(101.5))
				pool.Del(key)
			}
		})

		It("setnx", func() {
			key := "setnx_key"
			for _, pool := range pools {
				Expect(pool.SetNX(key, "bar", 0).Val()).To(Equal(true))
				Expect(pool.SetNX(key, "bar", 0).Val()).To(Equal(false))
				pool.Del(key)
			}
		})

		It("setxx", func() {
			key := "setxx_key"
			for _, pool := range pools {
				Expect(pool.SetXX(key, "bar", 0).Val()).To(Equal(false))
				Expect(pool.Set(key, 100, 0).Err()).NotTo(HaveOccurred())
				Expect(pool.SetNX(key, "bar", 0).Val()).To(Equal(false))
				pool.Del(key)
			}
		})

		It("setrange", func() {
			key := "setrange_key"
			for _, pool := range pools {
				Expect(pool.Set(key, "hello,world", 0).Err()).NotTo(HaveOccurred())
				Expect(pool.SetRange(key, 6, "myworld").Err()).NotTo(HaveOccurred())
				time.Sleep(10 * time.Millisecond)
				Expect(pool.Get(key).Val()).To(Equal("hello,myworld"))
				pool.Del(key)
			}
		})

		It("strlen", func() {
			key := "strlen_key"
			for _, pool := range pools {
				Expect(pool.Set(key, "hello", 0).Err()).NotTo(HaveOccurred())
				time.Sleep(10 * time.Millisecond)
				Expect(pool.StrLen(key).Val()).To(Equal(int64(5)))
				pool.Del(key)
			}
		})

		It("hset/hget", func() {
			key := "hset_key"
			field := "filed"
			for _, pool := range pools {
				Expect(pool.HSet(key, field, "bar").Val()).To(Equal(true))
				time.Sleep(10 * time.Millisecond)
				Expect(pool.HGet(key, field).Val()).To(Equal("bar"))
				pool.Del(key)
			}
		})

		It("hexists", func() {
			key := "hexists_key"
			field := "filed"
			for _, pool := range pools {
				Expect(pool.HSet(key, field, "bar").Val()).To(Equal(true))
				time.Sleep(10 * time.Millisecond)
				Expect(pool.HExists(key, field).Val()).To(Equal(true))
				Expect(pool.HDel(key, field).Val()).To(Equal(int64(1)))
				time.Sleep(10 * time.Millisecond)
				Expect(pool.HExists(key, field).Val()).To(Equal(false))
				pool.Del(key)
			}
		})

		It("hgetall", func() {
			key := "hgetall_key"
			fvs := []string{"f1", "v1", "f2", "v2", "f3", "v3"}
			for _, pool := range pools {
				Expect(pool.HMSet(key, fvs).Val()).To(Equal(int64(len(fvs) / 2)))
				time.Sleep(10 * time.Millisecond)
				kvs := pool.HGetAll(key).Val()
				for i := 0; i < len(fvs); i += 2 {
					Expect(kvs[fvs[i]]).To(Equal(fvs[i+1]))
				}
				Expect(len(pool.HKeys(key).Val())).To(Equal(len(fvs) / 2))
				Expect(len(pool.HVals(key).Val())).To(Equal(len(fvs) / 2))
				pool.Del(key)
			}
		})

		It("hmset/hmget", func() {
			key := "hmset_key"
			fvs := []string{"f1", "v1", "f2", "v2", "f3", "v3"}
			fields := make([]string, len(fvs)/2)
			for i := 0; i < len(fvs); i += 2 {
				fields[i/2] = fvs[i]
			}
			for _, pool := range pools {
				Expect(pool.HMSet(key, fvs).Val()).To(Equal(int64(len(fvs) / 2)))
				time.Sleep(10 * time.Millisecond)
				vals := pool.HMGet(key, fields...).Val()
				Expect(len(vals)).To(Equal(len(fvs) / 2))
				Expect(pool.HLen(key).Val()).To(Equal(int64(len(fvs) / 2)))
				pool.Del(key)
			}
		})

		It("hincrby", func() {
			key := "hincrby_key"
			intField := "int_field"
			floatField := "float_field"
			for _, pool := range pools {
				Expect(pool.HIncrBy(key, intField, 100).Val()).To(Equal(int64(100)))
				Expect(pool.HIncrBy(key, intField, 100).Val()).To(Equal(int64(200)))
				Expect(pool.HIncrByFloat(key, floatField, 10.5).Val()).To(Equal(float64(10.5)))
				Expect(pool.HIncrByFloat(key, floatField, 10.5).Val()).To(Equal(float64(21)))
				Expect(pool.HDel(key, intField, floatField).Val()).To(Equal(int64(2)))
				pool.Del(key)
			}
		})

		It("blpop/brpop", func() {
			key := "blpop_key"
			noExistsKey := "non_exists_key"
			for _, pool := range pools {
				go func() {
					time.Sleep(100 * time.Millisecond)
					pool.LPush(key, "e1", "e2")
				}()
				Expect(pool.BLPop(time.Second, key).Val()).To(Equal([]string{key, "e2"}))
				Expect(pool.BLPop(time.Second, key).Val()).To(Equal([]string{key, "e1"}))
				if pool == shardPool {
					Expect(pool.BLPop(time.Second, key, noExistsKey).Err()).To(HaveOccurred())
				}
			}
		})

		It("brpop", func() {
			key := "brpop_key"
			noExistsKey := "non_exists_key"
			for _, pool := range pools {
				go func() {
					time.Sleep(100 * time.Millisecond)
					pool.LPush(key, "e1", "e2")
				}()
				Expect(pool.BRPop(time.Second, key).Val()).To(Equal([]string{key, "e1"}))
				Expect(pool.BRPop(time.Second, key).Val()).To(Equal([]string{key, "e2"}))
				if pool == shardPool {
					Expect(pool.BRPop(time.Second, key, noExistsKey).Err()).To(HaveOccurred())
				}
			}
		})

		It("brpoplpush", func() {
			sourceKey := "brpoplpush_source"
			destKey := "brpoplpush_destination"
			crossShardKey := "cross_shard_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				go func() {
					time.Sleep(100 * time.Millisecond)
					pool.LPush(sourceKey, elems)
				}()
				for _, elem := range elems {
					Expect(pool.BRPopLPush(sourceKey, destKey, time.Second).Val()).To(Equal(elem))
				}
				if pool == shardPool {
					Expect(pool.BRPop(time.Second, sourceKey, crossShardKey).Err()).To(HaveOccurred())
				}
				pool.Del(sourceKey, destKey)
			}
		})

		It("lindex", func() {
			key := "lindex_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				pool.RPush(key, elems)
				time.Sleep(10 * time.Millisecond)
				for i, elem := range elems {
					Expect(pool.LIndex(key, int64(i)).Val()).To(Equal(elem))
				}
				pool.Del(key)
			}
		})

		It("linsert", func() {
			key := "linsert_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				pool.RPush(key, elems)
				Expect(pool.LInsertBefore(key, "e1", "hello").Val()).
					To(Equal(int64(len(elems) + 1)))
				Expect(pool.LInsertBefore(key, "e0", "hello").Val()).
					To(Equal(int64(-1)))
				Expect(pool.LInsertAfter(key, "hello", "world").Val()).
					To(Equal(int64(len(elems) + 2)))
				Expect(pool.LLen(key).Val()).To(Equal(int64(len(elems) + 2)))
				pool.Del(key)
			}
		})

		It("lpush/rpop", func() {
			key := "lpush_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				Expect(pool.LPush(key, elems).Val()).To(Equal(int64(len(elems))))
				for _, elem := range elems {
					Expect(pool.RPop(key).Val()).To(Equal(elem))
				}
			}
		})

		It("rpush/lpop", func() {
			key := "rpush_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				Expect(pool.RPush(key, elems).Val()).To(Equal(int64(len(elems))))
				for _, elem := range elems {
					Expect(pool.LPop(key).Val()).To(Equal(elem))
				}
			}
		})

		It("lpushx", func() {
			key := "lpushx_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				Expect(pool.LPushX(key, elems).Val()).To(Equal(int64(0)))
				pool.LPush(key, "e0")
				Expect(pool.LPushX(key, elems).Val()).To(Equal(int64(len(elems) + 1)))
				Expect(pool.RPop(key).Val()).To(Equal("e0"))
				for _, elem := range elems {
					Expect(pool.RPop(key).Val()).To(Equal(elem))
				}
			}
		})

		It("rpushx", func() {
			key := "rpushx_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				Expect(pool.RPushX(key, elems).Val()).To(Equal(int64(0)))
				pool.RPush(key, "e0")
				Expect(pool.RPushX(key, elems).Val()).To(Equal(int64(len(elems) + 1)))
				Expect(pool.LPop(key).Val()).To(Equal("e0"))
				for _, elem := range elems {
					Expect(pool.LPop(key).Val()).To(Equal(elem))
				}
			}
		})

		It("lrange", func() {
			key := "lrange_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				Expect(pool.RPush(key, elems).Val()).To(Equal(int64(len(elems))))
				time.Sleep(10 * time.Millisecond)
				Expect(pool.LRange(key, 0, -1).Val()).To(Equal(elems))
				pool.Del(key)
			}
		})

		It("lrem", func() {
			key := "lrem_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				Expect(pool.RPush(key, elems).Val()).To(Equal(int64(len(elems))))
				for _, elem := range elems {
					Expect(pool.LRem(key, 0, elem).Val()).To(Equal(int64(1)))
				}
			}
		})

		It("lset", func() {
			key := "lset_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				Expect(pool.RPush(key, elems).Val()).To(Equal(int64(len(elems))))
				Expect(pool.LSet(key, 0, "hello").Val()).To(Equal("OK"))
				pool.Del(key)
			}
		})

		It("ltrim", func() {
			key := "ltrim_key"
			elems := []string{"e1", "e2", "e3"}
			for _, pool := range pools {
				Expect(pool.RPush(key, elems).Val()).To(Equal(int64(len(elems))))
				Expect(pool.LTrim(key, 1, -1).Val()).To(Equal("OK"))
				Expect(pool.LRange(key, 0, -1).Val()).To(Equal(elems[1:]))
				pool.Del(key)
			}
		})

		It("rpoplpush", func() {
			sourceKey := "rpoplpush_source"
			destKey := "rpoplpush_destination"
			for _, pool := range pools {
				ret, err := pool.RPopLPush(sourceKey, destKey).Result()
				Expect(err).NotTo(Equal(HaveOccurred))
				Expect(ret).To(Equal(""))
				pool.LPush(sourceKey, "hello")
				ret, err = pool.RPopLPush(sourceKey, destKey).Result()
				Expect(err).NotTo(Equal(HaveOccurred))
				Expect(ret).To(Equal("hello"))
			}
		})
	})
})
