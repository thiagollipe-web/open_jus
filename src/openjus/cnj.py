"""Número único CNJ (Res. CNJ 65/2008): NNNNNNN-DD.AAAA.J.TT.OOOO e mapeamento p/ índice DataJud."""

import re
from dataclasses import dataclass

CNJ_RE = re.compile(r"^(\d{7})-?(\d{2})\.?(\d{4})\.?(\d)\.?(\d{2})\.?(\d{4})$")

_UF_TJ = {  # TT da Justiça Estadual (J=8) -> sigla do TJ
    1: "tjac", 2: "tjal", 3: "tjap", 4: "tjam", 5: "tjba", 6: "tjce", 7: "tjdft", 8: "tjes",
    9: "tjgo", 10: "tjma", 11: "tjmt", 12: "tjms", 13: "tjmg", 14: "tjpa", 15: "tjpb",
    16: "tjpr", 17: "tjpe", 18: "tjpi", 19: "tjrj", 20: "tjrn", 21: "tjrs", 22: "tjro",
    23: "tjrr", 24: "tjsc", 25: "tjse", 26: "tjsp", 27: "tjto",
}


_MIL = {13: "tjmmg", 21: "tjmrs", 26: "tjmsp"}  # J=9 Justiça Militar Estadual


class CNJError(ValueError):
    pass


@dataclass(frozen=True)
class NumeroCNJ:
    sequencial: str
    dv: str
    ano: str
    segmento: int
    tribunal: int
    origem: str

    @property
    def formatado(self) -> str:
        return f"{self.sequencial}-{self.dv}.{self.ano}.{self.segmento}.{self.tribunal:02d}.{self.origem}"

    @property
    def limpo(self) -> str:
        return f"{self.sequencial}{self.dv}{self.ano}{self.segmento}{self.tribunal:02d}{self.origem}"

    @property
    def alias_datajud(self) -> str:
        """Alias do índice na API Pública (api_publica_<alias>)."""
        j, tt = self.segmento, self.tribunal
        if j == 1:
            raise CNJError("STF não é coberto pela API Pública do DataJud.")
        if j == 3 and tt == 0:
            return "stj"
        if j == 5 and tt == 0:
            return "tst"
        if j == 6 and tt == 0:
            return "tse"
        if j == 7 and tt == 0:
            return "stm"
        if j == 4 and 1 <= tt <= 6:
            return f"trf{tt}"
        if j == 5 and 1 <= tt <= 24:
            return f"trt{tt}"
        if j == 6 and tt in _UF_TJ:
            return "tre-" + _UF_TJ[tt][2:]
        if j == 8 and tt in _UF_TJ:
            return _UF_TJ[tt]
        if j == 9 and tt in _MIL:
            return _MIL[tt]
        raise CNJError(f"Combinação J={j} TT={tt:02d} sem índice DataJud conhecido.")


def dv_valido(sequencial: str, dv: str, ano: str, j: int, tt: int, origem: str) -> bool:
    """Módulo 97 base 10 (ISO 7064): resto de NNNNNNNAAAAJTTOOOO00 -> DV = 98 - resto."""
    base = f"{sequencial}{ano}{j}{tt:02d}{origem}00"
    return int(dv) == 98 - (int(base) % 97)


def parse_cnj(texto: str, validar_dv: bool = True) -> NumeroCNJ:
    m = CNJ_RE.match(texto.strip())
    if not m:
        raise CNJError("Formato inválido. Use NNNNNNN-DD.AAAA.J.TT.OOOO.")
    seq, dv, ano, j, tt, org = m.groups()
    n = NumeroCNJ(seq, dv, ano, int(j), int(tt), org)
    if validar_dv and not dv_valido(seq, dv, ano, int(j), int(tt), org):
        raise CNJError("Dígito verificador (DD) inválido.")
    return n
