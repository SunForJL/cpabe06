package bswabe

import (
	"github.com/Nik-U/pbc"
	"fmt"
	"strings"
	"strconv"
	"crypto/sha1"
)

type BswabePub struct {
	//public key
	pairingDesc string
	p *pbc.Pairing
	g *pbc.Element				/* G_1 */
	h *pbc.Element				/* G_1 */
	//f *pbc.Element			/* G_1 */
	gp *pbc.Element				/* G_2 */
	g_hat_alpha *pbc.Element	/* G_T */
}

type BswabeMsk struct {
	//master secret key
	beta *pbc.Element  	  /* Z_r */
	g_alpha *pbc.Element  /* G_2 */
}

type BswabeCph struct {
/*
 * A ciphertext. Note that this library only handles encrypting a single
 * group element, so if you want to encrypt something bigger, you will have
 * to use that group element as a symmetric key for hybrid encryption (which
 * you do yourself).
 */
	cs *pbc.Element  /* G_T */
	c *pbc.Element /* G_1 */
	p *BswabePolicy
}

type BswabeCphKey struct {
	cph *BswabeCph
	key *pbc.Element
	ciphertext []byte
}

type BswabeElementBoolean struct {
	e *pbc.Element
	b bool
}

type BswabePolicy struct {
	/* k=1 if leaf, otherwise threshould */
	k int
	/* attribute string if leaf, otherwise null */
	attr string
	c *pbc.Element			/* G_1 only for leaves */
	cp *pbc.Element		/* G_1 only for leaves */
	/* array of BswabePolicy and length is 0 for leaves */
	children []*BswabePolicy
	/* only used during encryption */
	q *BswabePolynomial

	/* only used during decription */
	satisfiable bool
	min_leaves int
	attri int
	satl []int
}

type BswabePolynomial struct {
	deg int
	/* coefficients from [0] x^0 to [deg] x^deg */
	coef []*pbc.Element /* G_T (of length deg+1) */
}

type BswabePrv struct {
 	//*private key
	d *pbc.Element  /* G_2 */
	comps []*BswabePrvComp /* BswabePrvComp */
}

type BswabePrvComp struct {
	/* these actually get serialized */
	attr string
	d *pbc.Element					/* G_2 */
	dp *pbc.Element 				/* G_2 */

	/* only used during dec */
	used int
	z *pbc.Element			/* G_1 */
	zp *pbc.Element			/* G_1 */
}

// * Generate a public key and corresponding master secret key.
var curveParams string = "type a\n" + "q 87807107996633125224377819847540498158068831994142082" + "1102865339926647563088022295707862517942266222142315585" + "8769582317459277713367317481324925129998224791\n" + "h 12016012264891146079388821366740534204802954401251311" + "822919615131047207289359704531102844802183906537786776\n" + "r 730750818665451621361119245571504901405976559617\n" + "exp2 159\n" + "exp1 107\n" + "sign1 1\n" + "sign0 1\n"

func Setup(pub *BswabePub, msk *BswabeMsk ) {
	params := new(pbc.Params)
	params,_ = pbc.NewParamsFromString(curveParams)

	pub.pairingDesc = curveParams
	pub.p = pbc.NewPairing(params)
	pairing := pub.p

	pub.g = pairing.NewG1().Rand()
	//pub.f = pairing.NewG1()
	pub.h = pairing.NewG1()
	pub.gp = pairing.NewG2().Rand()
	pub.g_hat_alpha = pairing.NewGT()
	alpha := pairing.NewZr().Rand()
	msk.beta = pairing.NewZr().Rand()
	msk.g_alpha = pairing.NewG2()

	msk.g_alpha = pub.gp.NewFieldElement().Set(pub.gp).ThenPowZn(alpha)
	//beta_inv := msk.beta.NewFieldElement().Set(msk.beta).ThenInvert()
	//pub.f = pub.g.NewFieldElement().Set(pub.g).ThenPowZn(beta_inv)
	pub.h = pub.g.NewFieldElement().Set(pub.g).ThenPowZn(msk.beta)
	pub.g_hat_alpha.Pair(pub.g, msk.g_alpha)
}

// * Generate a private key with the given set of attributes.
func Keygen(pub *BswabePub, msk *BswabeMsk, attrs []string) *BswabePrv {
	var prv = new(BswabePrv)

	/* initialize */
	pairing := pub.p
	prv.d = pairing.NewG2()
	g_r := pairing.NewG2()
	r := pairing.NewZr().Rand()
	beta_inv := pairing.NewZr()

	/* compute */
	g_r = pub.gp.NewFieldElement().Set(pub.gp).ThenPowZn(r)
	beta_inv = msk.beta.NewFieldElement().Set(msk.beta).ThenInvert()
	prv.d = msk.g_alpha.NewFieldElement().Set(msk.g_alpha).ThenMul(g_r).ThenPowZn(beta_inv)

	for i := 0; i < len(attrs); i++ {
		var comp = new(BswabePrvComp)

		comp.attr = attrs[i]
		comp.d = pairing.NewG2()
		comp.dp = pairing.NewG1()

		h_rp := pairing.NewG2()
		rp := pairing.NewZr().Rand() //对每个属性进行计算

		elementFromString(h_rp, comp.attr)
		h_rp.PowZn(h_rp,rp)
		comp.d = g_r.NewFieldElement().Set(g_r).ThenMul(h_rp)
		comp.dp = pub.g.NewFieldElement().Set(pub.g).ThenPowZn(rp)
		//fmt.Println(comp.d.Bytes())
		//fmt.Println(comp.dp.Bytes())

		prv.comps = append(prv.comps, comp)
		//fmt.Println(prv.comps[i].d.Bytes())
		//fmt.Println(prv.comps[i].dp.Bytes())
	}
	return prv
}

/*
 * Pick a random group element and encrypt it under the specified access
 * policy. The resulting ciphertext is returned and the Element given as an
 * argument (which need not be initialized) is set to the random group
 * element.
 *
 * After using this function, it is normal to extract the random data in m
 * using the pbc functions element_length_in_bytes and element_to_bytes and
 * use it as a key for hybrid encryption.
 *
 * The policy is specified as a simple string which encodes a postorder
 * traversal of threshold tree defining the access policy. As an example,
 *
 * "foo bar fim 2of3 baf 1of2"
 *
 * specifies a policy with two threshold gates and four leaves. It is not
 * possible to specify an attribute with whitespace in it (although "_" is
 * allowed).
 *
 * Numerical attributes and any other fancy stuff are not supported.
 *
 * Returns null if an error occured, in which case a description can be
 * retrieved by calling bswabe_error().
 */
func Enc( pub *BswabePub, policy string) *BswabeCphKey {
	keyCph := new(BswabeCphKey)
	cph := new(BswabeCph)
	/* initialize */
	pairing := pub.p
	s := pairing.NewZr().Rand()

	fmt.Println("s: ",s.Bytes())

	m := pairing.NewGT().Rand()
	cph.cs = pairing.NewGT()
	cph.c = pairing.NewG1()

	cph.p = parsePolicyPostfix(policy)
//	fmt.Println(cph.p.children[0].children[0].attr)
	/* compute */
	cph.cs = pub.g_hat_alpha.NewFieldElement().Set(pub.g_hat_alpha).ThenPowZn(s).ThenMul(m)
	cph.c = pub.h.NewFieldElement().Set(pub.h).ThenPowZn(s)

	fillPolicy(cph.p, pub, s)

	keyCph.cph = cph
	keyCph.key = m.NewFieldElement().Set(m)
	return keyCph
}

/*
 * Decrypt the specified ciphertext using the given private key, filling in
 * the provided element m (which need not be initialized) with the result.
 *
 * Returns true if decryption succeeded, false if this key does not satisfy
 * the policy of the ciphertext (in which case m is unaltered).
 */
func Dec( pub *BswabePub, prv *BswabePrv, cph *BswabeCph) *BswabeElementBoolean {
	beb := new(BswabeElementBoolean)
	m := pub.p.NewGT()
	t := pub.p.NewGT()

	checkSatisfy(cph.p, prv) //检查属性集是否满足访问结构policy
	if !cph.p.satisfiable {
		fmt.Println("cannot decrypt, attributes in key do not satisfy policy")
		beb.e = nil
		beb.b = false
		return beb
	}

	//属性集符合policy要求，进一步解密
	pickSatisfyMinLeaves(cph.p, prv) //min_leaves salt

	decFlatten(t, cph.p, prv, pub)
	fmt.Println("t: ",t.Bytes())


	m = cph.cs.NewFieldElement().Set(cph.cs).ThenMul(t)

	t.NewFieldElement().Pair(cph.c, prv.d)
	t.Invert(t)
	m.ThenMul(t)

	beb.e = m.NewFieldElement().Set(m)
	beb.b = true
	return beb
}

func decFlatten(r *pbc.Element, p *BswabePolicy, prv *BswabePrv, pub *BswabePub) {
	one := pub.p.NewZr()
	one.Set1()

	//TODO r.set what?
	r.Set1()
	fmt.Println("r origion: ",r.Bytes())
	//r.Set0()
	decNodeFlatten(r, one, p, prv, pub)
}

func decNodeFlatten(r *pbc.Element, exp *pbc.Element, p *BswabePolicy, prv *BswabePrv, pub *BswabePub) {
	if p.children == nil || len(p.children) == 0 {
		decLeafFlatten(r, exp, p, prv, pub)
	} else {
		decInternalFlatten(r, exp, p, prv, pub)
	}
}

func decLeafFlatten(r *pbc.Element, exp *pbc.Element, p *BswabePolicy, prv *BswabePrv, pub *BswabePub) {
	//var c BswabePrvComp
	fmt.Println("r: ",r.Bytes())
	c := prv.comps[p.attri]
	s := pub.p.NewGT()
	t := pub.p.NewGT()

	s.Pair(p.c, c.d)
	t.Pair(p.cp, c.dp).ThenInvert()
	fmt.Println("exp: ",exp)
	s.ThenMul(t).ThenPowZn(exp)
	r.ThenMul(s)
	fmt.Println("r: ", r.Bytes())
}

func decInternalFlatten( r *pbc.Element, exp *pbc.Element, p *BswabePolicy, prv *BswabePrv, pub *BswabePub) {
	t := pub.p.NewZr()
	expnew := pub.p.NewZr()

	for i := 0; i < len(p.satl); i++ {
		lagrangeCoef(t, p.satl, p.satl[i])
		fmt.Println("exp: ", exp)
		expnew = exp.NewFieldElement().Set(exp).ThenMul(t)
		fmt.Println("expnew: ", expnew)
		decNodeFlatten(r, expnew, p.children[p.satl[i] - 1], prv, pub)
	}
}

func lagrangeCoef( r *pbc.Element, s []int, i int) {
	t := r.NewFieldElement().Set(r)
	r.Set1()

	for k := 0; k < len(s); k++ {
		fmt.Println("r: ",r)
		fmt.Println("t: ",t)
		j := s[k]
		if j == i {
			continue
		}
		t.SetInt32(int32(-j))
		r.Mul(r,t)
		fmt.Println("r: ",r)
		fmt.Println("t: ",t)

		t.SetInt32(int32(i-j)).ThenInvert()
		r.Mul(r,t)
		fmt.Println("r: ",r)
		fmt.Println("t: ",t)
	}
}

func pickSatisfyMinLeaves( p *BswabePolicy, prv *BswabePrv) {
	//var  i, k, l, c_i int
	var c []int

	if p.children == nil || len(p.children) == 0 {
		p.min_leaves = 1
	} else {
		for i := 0; i < len(p.children); i++ {
			if p.children[i].satisfiable {
				pickSatisfyMinLeaves(p.children[i], prv)
			}
		}

		for i := 0; i < len(p.children); i++ {
			c = append(c, i)
		}

		//Collections.sort(c, new IntegerComparator(p))
		//TODO 这里的排序需要进一步改写,min_leaves是从小到大排序的
		for i := 0; i < len(p.children); i++ {
			for j := 0; j < len(p.children)-i-1; j++ {
				//fmt.Println("p.children[j].min_leaves: ", p.children[c[j]].min_leaves)
				//fmt.Println("p.children[j+1].min_leaves: ", p.children[c[j+1]].min_leaves)
				if p.children[c[j]].min_leaves > p.children[c[j+1]].min_leaves {
					c[j], c[j+1] = c[j+1], c[j]
				}
			}
		}

		//p.satl = new ArrayList<Integer>()
		p.min_leaves = 0
		l := 0

		for i := 0; i < len(p.children) && l < p.k; i++ {
			c_i := c[i] /* c[i] */
			if p.children[c_i].satisfiable {
				l++
				p.min_leaves += p.children[c_i].min_leaves
				k := c_i + 1
				p.satl = append(p.satl, k)
			}
		}
	}
}

func checkSatisfy( p *BswabePolicy, prv *BswabePrv) {
	var i, l int
	var prvAttr string

	p.satisfiable = false
	if p.children == nil || len(p.children) == 0 {
		for i = 0; i < len(prv.comps); i++ {
			prvAttr = prv.comps[i].attr
			if strings.Compare(prvAttr,p.attr) == 0 {
				p.satisfiable = true
				p.attri = i
				break
			}
		}
	} else {
		for i = 0; i < len(p.children); i++ {
			checkSatisfy(p.children[i], prv)
		}
		l = 0
		for i = 0; i < len(p.children); i++ {
			if p.children[i].satisfiable {
				l++
			}
		}

		if l >= p.k {
			p.satisfiable = true
		}
	}
}

func fillPolicy( p *BswabePolicy, pub *BswabePub, s *pbc.Element)  {
	pairing := pub.p
	r := pairing.NewZr()
	t := pairing.NewZr() //子节点的q（0）
	h := pairing.NewG2()

	p.q = randPoly(p.k - 1, s)

	fmt.Println("attr: ", p.attr)
	for i:=0; i<len(p.q.coef); i++{
		fmt.Println("coef",i,": ",p.q.coef[i].Bytes())
	}
	//if len(p.q.coef) == 2{
	//	fmt.Println("[0]+[1]: ", (p.q.coef[0].NewFieldElement().Set(p.q.coef[0]).ThenAdd(p.q.coef[1])).Bytes())
	//	fmt.Println("[0]+2*[1]: ", (p.q.coef[0].NewFieldElement().Set(p.q.coef[0]).ThenAdd(p.q.coef[1]).ThenAdd(p.q.coef[1])).Bytes())
	//	fmt.Println("[0]+3*[1]: ", (p.q.coef[0].NewFieldElement().Set(p.q.coef[0]).ThenAdd(p.q.coef[1]).ThenAdd(p.q.coef[1]).ThenAdd(p.q.coef[1])).Bytes())
	//}

	if p.children == nil || len(p.children) == 0 {
		p.c = pairing.NewG1()
		p.cp = pairing.NewG2()

		p.c = pub.g.NewFieldElement().Set(pub.g).ThenPowZn(p.q.coef[0])
		elementFromString(h, p.attr)
		p.cp = h.NewFieldElement().Set(h).ThenPowZn(p.q.coef[0])
		//fmt.Println("C: ",p.c.Bytes())
		//fmt.Println("CP: ",p.cp.Bytes())

	} else {
		for i := 0; i < len(p.children); i++ {
			r.SetInt32(int32(i + 1))
			evalPoly(t, p.q, r)
			fillPolicy(p.children[i], pub, t)
		}
	}
}

func evalPoly( r *pbc.Element, q *BswabePolynomial, x *pbc.Element) {
	s := r.NewFieldElement().Set(r)
	t := r.NewFieldElement().Set(r)

	r.Set0()
	t.Set1()

	for i := 0; i < q.deg + 1; i++ {
		//fmt.Println("s: ",s.Bytes())
		//fmt.Println("r: ",r.Bytes())
		//fmt.Println("t: ",t.Bytes())
		//fmt.Println("x: ",x.Bytes())
		/* r += q->coef[i] * t */
		s = q.coef[i].NewFieldElement().Set(q.coef[i])
		s.ThenMul(t)
		r.Add(r,s)

		/* t *= x */
		t.ThenMul(x)
	}
}

func randPoly( deg int, zeroVal *pbc.Element) *BswabePolynomial {
	q := new(BswabePolynomial)
	q.deg = deg
	q.coef = make([]*pbc.Element,deg+1)

	for i := 0; i < deg + 1; i++ {
		q.coef[i] = zeroVal.NewFieldElement().Set(zeroVal)
	}
	q.coef[0].Set(zeroVal)

	for i := 1; i < deg + 1; i++ {
		//TODO 随机化coef[1]会使得coef[0]的值也改变成一样的值
		// 解决方法：将其地址指向不同的element
		q.coef[i].Rand()
	}
	return q
}

func parsePolicyPostfix(s string) *BswabePolicy {
	var toks []string
	var tok string
	var stack []*BswabePolicy
	//var root *BswabePolicy

	toks = strings.Split(s," ")

	toks_cnt := len(toks)
	for index := 0; index < toks_cnt; index++ {
		var i int

		tok = toks[index]
		if tok == "" {
			continue
		}

		if !strings.Contains(tok,"of") {
			stack = append(stack, baseNode(1, tok))
		} else {
			//var node *BswabePolicy
			node := new(BswabePolicy)

			/* parse kof n node */
			var k_n []string = strings.Split(tok,"of")
			k,error := strconv.Atoi(k_n[0])
			n,error := strconv.Atoi(k_n[1])
			if error != nil {
				fmt.Println("字符串转换成整数失败")
			}

			if k < 1 {
				fmt.Println("error parsing " + s + ": trivially satisfied operator " + tok)
				return nil
			} else if k > n {
				fmt.Println("error parsing " + s + ": unsatisfiable operator " + tok)
				return nil
			} else if n == 1 {
				fmt.Println("error parsing " + s + ": indentity operator " + tok)
				return nil
			} else if n > len(stack) {
				fmt.Println("error parsing " + s + ": stack underflow at " + tok)
				return nil
		}

		/* pop n things and fill in children */
		node = baseNode(k, "")
		node.children = make([]*BswabePolicy,n)

		for i = n - 1; i >= 0; i-- {
			node.children[i] = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, node)
		}
	}

	//fmt.Println(stack[0].children[0].children[0].attr)

	if len(stack) > 1 {
		fmt.Println("error parsing " + s + ": extra node left on the stack")
		return nil
	} else if len(stack) < 1 {
		fmt.Println("error parsing " + s + ": empty policy")
		return nil
	}

	return stack[0]
	//root = stack[0]
	//return root
}

func baseNode( k int, s string) *BswabePolicy {
	p := new(BswabePolicy)

	p.k = k
	//p.attr = s
	if !(s == "") {
		p.attr = s
	} else {
		p.attr = ""
	}
	p.q = nil
	return p
}

func elementFromString( h *pbc.Element, s string) {
	sha := sha1.Sum([]byte(s))
	digest := sha[:]
	h.SetFromHash(digest)
}
