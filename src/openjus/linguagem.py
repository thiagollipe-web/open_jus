"""Camada 4 — traduz 'juridiquês' das movimentações do DataJud e filtra clichês de IA."""

from __future__ import annotations

import re

GLOSSARIO: dict[str, str] = {
    "conclusos para despacho": "O processo foi enviado ao juiz para uma decisão simples (despacho).",
    "conclusos para decisão": "O processo foi enviado ao juiz para que ele decida algo.",
    "conclusos para sentença": "O processo está com o juiz para a decisão final (sentença).",
    "juntada de petição": "Um novo documento/pedido de uma das partes foi anexado ao processo.",
    "juntada de mandado": "O resultado de uma ordem cumprida pelo oficial de justiça foi anexado.",
    "expedição de mandado": "Foi emitida uma ordem para o oficial de justiça cumprir.",
    "expedição de alvará": "Foi emitida autorização para levantar (retirar) valores depositados.",
    "citação": "A parte ré foi comunicada oficialmente de que o processo existe.",
    "intimação": "Alguém foi comunicado oficialmente de um ato para que possa reagir (ou cumprir).",
    "trânsito em julgado": "A decisão não admite mais recurso; tornou-se definitiva.",
    "baixa definitiva": "O processo foi encerrado e arquivado.",
    "remessa": "O processo foi enviado a outro órgão ou instância.",
    "distribuição": "O processo foi sorteado/encaminhado a uma vara ou juiz.",
    "audiência designada": "Foi marcada uma audiência.",
    "publicação": "A decisão foi divulgada no Diário da Justiça.",
    "tutela de urgência": "Pedido/decisão provisória, dada com rapidez antes do fim do processo.",
    "liminar": "Decisão provisória, tomada logo no início do processo.",
    "sentença": "Decisão do juiz que encerra a fase de conhecimento do processo.",
    "acórdão": "Decisão tomada por um grupo de juízes (tribunal).",
    "agravo": "Recurso contra uma decisão do juiz durante o processo.",
    "apelação": "Recurso contra a sentença.",
    "embargos de declaração": "Pedido para esclarecer ponto obscuro, omisso ou contraditório da decisão.",
    "cumprimento de sentença": "Fase em que se exige o que a sentença determinou.",
    "segredo de justiça": "O processo tramita sob sigilo; só as partes e advogados têm acesso.",
}

CLICHES_IA = [
    r"\bcomo (um )?modelo de linguagem\b[^.]*\.\s*",
    r"\bé importante (ressaltar|destacar|notar) que\s*",
    r"\bem suma,?\s*",
    r"\bvale (ressaltar|destacar) que\s*",
    r"\bnão sou (um )?advogad[oa][^.]*\.\s*",
    r"\bespero que isso ajude[^.]*\.?\s*",
]


def traduzir_movimento(nome: str) -> str:
    """Retorna explicação em linguagem simples para um nome de movimentação."""
    baixo = nome.lower()
    achados = [v for k, v in sorted(GLOSSARIO.items(), key=lambda kv: -len(kv[0])) if k in baixo]
    return achados[0] if achados else "Andamento processual (sem tradução cadastrada)."


def remover_cliches(texto: str) -> str:
    for padrao in CLICHES_IA:
        texto = re.sub(padrao, "", texto, flags=re.IGNORECASE)
    texto = re.sub(r"[ \t]{2,}", " ", texto)
    return texto.strip()
